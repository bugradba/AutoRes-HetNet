package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// ============================================================
// HATA 3 DÜZELTMESİ: Monte Carlo + Güven Aralığı
//
// Eski tasarımın iki kusuru vardı:
//   1. Tüm metrikler TEK bir koşudan raporlanıyordu. Sistem stokastik
//      (rastgele konum + shadowing + asenkron mesaj yarışları) olduğu
//      için her koşu farklı sonuç verir; tek örnek temsil edici değildir.
//   2. Koşu, duvar saatiyle (sabit 15 s) bitiyordu. Bu mantıksal bir
//      yakınsama koşulu değildir: bazı istasyonlar FAILED kalabilir ve
//      projenin asıl katkısı olan "yakınsama süresi" hiç ölçülemez.
//
// Bu dosya ikisini de düzeltir:
//   - BuildNetwork: tohumlu rng ile TEKRARLANABİLİR topoloji kurar.
//   - RunSimulation: sabit süre yerine "tüm istasyonlar COMMITTED"
//     mantıksal koşulunu bekler (güvenlik için üst süre sınırıyla)
//     ve yakınsama süresini ölçer.
//   - RunMonteCarlo: koşuları tekrarlar, her metrik için
//     ortalama ± %95 güven aralığı (mean ± 1.96·σ/√n) raporlar.
// ============================================================

// BuildNetwork: eski main() içindeki topoloji kurulumunun, tohumlu
// rng kullanan tekrarlanabilir hali. Aynı seed => aynı topoloji.
func BuildNetwork(rng *rand.Rand, n int, areaSize, threshold float64, verbose bool) []*BaseStation {
	net := make([]*BaseStation, n)
	for i := 0; i < n; i++ {
		net[i] = NewBaseStation(Agent_ID(i), rng.Float64()*areaSize, rng.Float64()*areaSize)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dist := Distance(net[i], net[j])

			// Eşik değer kontrolü (Menzil dışındaysa hesaplama yapma)
			if dist < threshold {
				baseWeight := ReferenceLoss * math.Pow(dist, -PathLossExponent)
				shadowing := lognormalShadow(rng) // PHY ile aynı gölgeleme modeli (σ tek kaynaktan)
				finalWeight := baseWeight * shadowing

				net[i].NeighborWeights[net[j].ID] = finalWeight
				net[j].NeighborWeights[net[i].ID] = finalWeight

				net[i].Neighbros = append(net[i].Neighbros, net[j].ID)
				net[j].Neighbros = append(net[j].Neighbros, net[i].ID)

				net[i].Outbox[net[j].ID] = net[j].Inbox
				net[j].Outbox[net[i].ID] = net[i].Inbox

				if verbose {
					fmt.Printf("Link: BS-%d <--> BS-%d | Dist: %.1fm | Shadowing: %.2fx | Final Weight: %.3e\n",
						i, j, dist, shadowing, finalWeight)
				}
			}
		}
	}
	return net
}

// AllCommitted: tüm istasyonlar COMMITTED mi? (Mutex ile güvenli okuma;
// ajan goroutine'leri hâlâ çalışırken çağrılır.)
func AllCommitted(net []*BaseStation) bool {
	for _, bs := range net {
		bs.Mutex.Lock()
		st := bs.State
		bs.Mutex.Unlock()
		if st != STATE_COMMITTED {
			return false
		}
	}
	return true
}

// CommittedCount: COMMITTED istasyon sayısı.
func CommittedCount(net []*BaseStation) int {
	c := 0
	for _, bs := range net {
		bs.Mutex.Lock()
		if bs.State == STATE_COMMITTED {
			c++
		}
		bs.Mutex.Unlock()
	}
	return c
}

// RunSimulation: ajanları başlatır, MANTIKSAL yakınsamayı bekler
// (duvar saati DEĞİL), sonra hepsini temiz biçimde durdurur.
// Dönenler: yakınsama süresi (saniye) ve yakınsadı mı bilgisi.
// maxWait: livelock/kilitlenme ihtimaline karşı güvenlik üst sınırı.
func RunSimulation(net []*BaseStation, maxWait time.Duration) (convSec float64, converged bool) {
	var wg sync.WaitGroup
	wg.Add(len(net))

	start := time.Now()
	for _, bs := range net {
		go bs.Start(&wg)
	}

	deadline := start.Add(maxWait)
	for {
		time.Sleep(50 * time.Millisecond) // yoklama aralığı
		if AllCommitted(net) {
			converged = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	convSec = time.Since(start).Seconds()

	for _, bs := range net {
		bs.Stop()
	}
	wg.Wait()

	// Yakınsamadan bittiyse: Think() içindeki 2 s'lik commit zamanlayıcıları
	// hâlâ bekliyor olabilir; metrikleri yarışsız okumak için süre tanı.
	if !converged {
		time.Sleep(2200 * time.Millisecond)
	}
	return convSec, converged
}

// ------------------- İSTATİSTİK YARDIMCILARI -------------------

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stdDev(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	m := mean(xs)
	ss := 0.0
	for _, x := range xs {
		d := x - m
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1)) // örneklem std sapması
}

// ci95Half: %95 güven aralığı yarı genişliği = 1.96 · σ / √n
func ci95Half(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return math.NaN()
	}
	return 1.96 * stdDev(xs) / math.Sqrt(float64(n))
}

func reportStat(name string, xs []float64) {
	if len(xs) == 0 {
		fmt.Printf("%-32s: (veri yok)\n", name)
		return
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	fmt.Printf("%-32s: %.4g ± %.4g  [min %.4g, max %.4g]  (n=%d)\n",
		name, mean(xs), ci95Half(xs), lo, hi, len(xs))
}

// ------------------- MONTE CARLO ÇEKİRDEĞİ -------------------

// RunMonteCarlo: 'runs' adet bağımsız koşu yapar. Her koşuda:
//
//	seed = baseSeed + r  =>  topoloji tekrarlanabilir
//
// (Asenkron mesaj yarışları doğası gereği deterministik değildir;
// bu, ölçmek İSTEDİĞİMİZ stokastikliğin bir parçasıdır.)
func RunMonteCarlo(runs int, baseSeed int64, optBudget time.Duration) {
	fmt.Printf("--- MONTE CARLO: %d koşu (baseSeed=%d, N=%d, Alan=%.0fm, Eşik=%.0fm, K=%d) ---\n\n",
		runs, baseSeed, SimN, SimAreaSize, SimThreshold, MaxColors)

	type schemeAgg struct{ cost, thr, fair []float64 }
	schemeNames := []string{"Dağıtık (NE)", "Greedy", "DSATUR", "Sabit reuse", "Rastgele"}
	cmp := map[string]*schemeAgg{}
	for _, nm := range schemeNames {
		cmp[nm] = &schemeAgg{}
	}

	var (
		convTimes  []float64 // yalnızca yakınsayan koşular
		commFracs  []float64
		interfs    []float64
		gains      []float64 // gain over greedy (tanımlı olanlar)
		poas       []float64 // empirik PoA (yalnızca optimum kanıtlananlar ve OPT>0)
		avgCaps    []float64
		fairs      []float64
		zeroInterf int // girişimi ~0 olan koşu sayısı ("0.0000" iddiasının dürüst hali)
		convCount  int
		optProven  int
		optZeroNE  int // optimum 0 iken NE>0 kalan koşular (PoA=+Inf örnekleri)
	)
	const eps = 1e-15

	for r := 0; r < runs; r++ {
		rng := rand.New(rand.NewSource(baseSeed + int64(r)))
		net := BuildNetwork(rng, SimN, SimAreaSize, SimThreshold, false)

		convSec, converged := RunSimulation(net, 20*time.Second)

		// HATA 6 DÜZELTMESİ: kanal (UE konumları + tüm gölgelemeler) koşu
		// başına BİR KEZ çekilir; dağıtık NE ve tüm baseline şemalar AYNI
		// gerçekleme üzerinde değerlendirilir. Şemalar arası fairness/hız
		// farkı böylece tahsis kararına izole edilir.
		ch := DrawChannel(net, rng)

		neAssign, neServed := NEAssignment(net)
		thrNE := ComputeThroughputs(net, neAssign, neServed, ch)

		allServed := make([]bool, len(net))
		for i := range allServed {
			allServed[i] = true
		}
		schemes := []struct {
			name   string
			assign []PRB
			served []bool
		}{
			{"Dağıtık (NE)", neAssign, neServed},
			{"Greedy", GreedyAssignment(net), allServed},
			{"DSATUR", DSATURAssignment(net), allServed},
			{"Sabit reuse", FixedReuseAssignment(net, MaxColors), allServed},
			{"Rastgele", RandomAssignment(net, MaxColors, rng), allServed},
		}
		for s, sc := range schemes {
			var thr []float64
			if s == 0 {
				thr = thrNE
			} else {
				thr = ComputeThroughputs(net, sc.assign, sc.served, ch)
			}
			cmp[sc.name].cost = append(cmp[sc.name].cost, AssignmentCost(net, sc.assign))
			cmp[sc.name].thr = append(cmp[sc.name].thr, meanServed(thr, sc.served))
			cmp[sc.name].fair = append(cmp[sc.name].fair, JainOf(thr))
		}

		// HATA 4 DÜZELTMESİ: ortalama, hizmet veren (COMMITTED) istasyon
		// başına hesaplanır; FAILED istasyonlar 0 Mbps'tir ve paydaya girmez.
		committed := CommittedCount(net)
		avgCap := meanServed(thrNE, neServed)
		fair := JainOf(thrNE)
		obj := CalculateGlobalObjective(net)
		greedy := CalculateGreedyBaseline(net)
		opt := BruteForceOptimum(net, MaxColors, optBudget)

		commFrac := float64(committed) / float64(len(net))
		commFracs = append(commFracs, commFrac)
		interfs = append(interfs, obj)
		avgCaps = append(avgCaps, avgCap)
		fairs = append(fairs, fair)

		if converged {
			convCount++
			convTimes = append(convTimes, convSec)
		}
		if obj < eps {
			zeroInterf++
		}

		gain := math.NaN()
		if greedy > eps {
			gain = obj / greedy
			gains = append(gains, gain)
		} else if obj < eps {
			gain = 1.0
			gains = append(gains, gain)
		} // greedy~0 && obj>0: tanımsız, istatistiğe katma

		poa := math.NaN()
		if opt.Exact {
			optProven++
			switch {
			case opt.Cost > eps:
				poa = obj / opt.Cost
				poas = append(poas, poa)
			case obj < eps:
				poa = 1.0
				poas = append(poas, poa)
			default:
				optZeroNE++ // OPT=0, NE>0: oran sonsuz; ayrı sayılır
			}
		}

		fmt.Printf("Run %3d | conv %5.1fs (%v) | committed %3.0f%% | interf %.3e | gain %.3f | PoA>= %.3f (opt exact=%v)\n",
			r, convSec, converged, commFrac*100, obj, gain, poa, opt.Exact)
	}

	fmt.Printf("\n================ ÖZET (%d koşu) ================\n", runs)
	fmt.Println("Format: ortalama ± %95 GA (1.96·σ/√n)")
	reportStat("Yakınsama süresi (s)", convTimes)
	reportStat("COMMITTED oranı", commFracs)
	reportStat("Toplam girişim (W)", interfs)
	reportStat("Gain over Greedy", gains)
	reportStat("Empirik PoA alt sınırı", poas)
	reportStat("Ort. hız / COMMITTED (Mbps)*", avgCaps)
	reportStat("Jain fairness*", fairs)
	fmt.Printf("\nYakınsayan koşu                 : %d/%d (%.0f%%)\n", convCount, runs, 100*float64(convCount)/float64(runs))
	fmt.Printf("Girişimi ~0 olan koşu           : %d/%d (%.0f%%)  <-- \"0.0000\" iddiasının dürüst hali\n", zeroInterf, runs, 100*float64(zeroInterf)/float64(runs))
	fmt.Printf("Optimum kanıtlanan koşu         : %d/%d\n", optProven, runs)
	if optZeroNE > 0 {
		fmt.Printf("OPT=0 iken NE>0 kalan koşu      : %d (PoA bu örneklerde sonsuz)\n", optZeroNE)
	}
	fmt.Println("\n--- BASELINE KARŞILAŞTIRMASI (koşu başına AYNI topoloji + AYNI kanal) ---")
	fmt.Println("Kanal donmuş olduğundan sütunlar arası fark tahsis yönteminden kaynaklanır.")
	fmt.Printf("%-14s | %-22s | %-18s | %s\n", "Şema", "Maliyet (ort ± GA)", "Mbps/served", "Jain")
	for _, nm := range schemeNames {
		a := cmp[nm]
		fmt.Printf("%-14s | %10.3e ± %.2e | %7.1f ± %5.1f | %.3f ± %.3f\n",
			nm, mean(a.cost), ci95Half(a.cost), mean(a.thr), ci95Half(a.thr), mean(a.fair), ci95Half(a.fair))
	}

	fmt.Println("\n* Jain indeksi buradaki hız dağılımını BETİMLER; algoritma fairness'i")
	fmt.Println("  doğrudan optimize etmez ve değer kullanıcı yerleşiminin")
	fmt.Println("  stokastikliğini de içerir (bkz. Hata 6).")
}

// meanServed: yalnızca hizmetteki istasyonlar üzerinden ortalama hız
// (Hata 4: FAILED istasyonlar 0 Mbps'tir ve paydaya girmez).
func meanServed(thr []float64, served []bool) float64 {
	sum, n := 0.0, 0
	for i, x := range thr {
		if served[i] {
			sum += x
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
