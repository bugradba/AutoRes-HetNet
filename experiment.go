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
				shadowing := math.Exp(rng.NormFloat64() * 0.5)
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

// stationSnapshot: yakınsama tespiti için tek istasyonun anlık görüntüsü.
type stationSnapshot struct {
	state AgentState
	prb   PRB
}

// takeSnapshot: tüm ağın durum+renk anlık görüntüsü (Mutex ile güvenli).
func takeSnapshot(net []*BaseStation) []stationSnapshot {
	snap := make([]stationSnapshot, len(net))
	for i, bs := range net {
		bs.Mutex.Lock()
		snap[i] = stationSnapshot{state: bs.State, prb: bs.CurrentPRB}
		bs.Mutex.Unlock()
	}
	return snap
}

func snapshotsEqual(a, b []stationSnapshot) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func allCommittedSnap(snap []stationSnapshot) bool {
	for _, s := range snap {
		if s.state != STATE_COMMITTED {
			return false
		}
	}
	return true
}

// RunSimulation: ajanları başlatır, MANTIKSAL yakınsamayı bekler
// (duvar saati DEĞİL), sonra hepsini temiz biçimde durdurur.
//
// H-2 DÜZELTMESİ — yakınsama tanımı değişti:
// COMMITTED artık uç durum olmadığı için (istasyonlar daha iyi yanıt
// buldukça yeniden teklif verebilir) "herkes COMMITTED" anlık koşulu
// yakınsama kanıtı değildir. Yeni tanım (sessizlik penceresi):
//
//	"Tüm istasyonlar COMMITTED VE quietWindow boyunca hiçbir
//	 istasyonun durumu/rengi değişmedi."
//
// convSec, ağın fiilen durulduğu an (son gözlenen değişiklik) olarak
// raporlanır; bekleme penceresi süreye dahil edilmez.
// maxWait: livelock/kilitlenme ihtimaline karşı güvenlik üst sınırı.
func RunSimulation(net []*BaseStation, maxWait time.Duration) (convSec float64, converged bool) {
	// Pencere, COMMITTED denetim periyodundan (500 ms tick) ve commit
	// zaman aşımından (2 s) uzun olmalı ki "sessizlik" gerçekten
	// "kimsenin sapma isteği kalmadı" anlamına gelsin.
	const quietWindow = 3 * time.Second

	var wg sync.WaitGroup
	wg.Add(len(net))

	start := time.Now()
	for _, bs := range net {
		go bs.Start(&wg)
	}

	deadline := start.Add(maxWait)
	prev := takeSnapshot(net)
	lastChange := time.Now()

	for {
		time.Sleep(50 * time.Millisecond) // yoklama aralığı
		cur := takeSnapshot(net)
		if !snapshotsEqual(cur, prev) {
			prev = cur
			lastChange = time.Now()
		}
		if allCommittedSnap(cur) && time.Since(lastChange) >= quietWindow {
			converged = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	convSec = lastChange.Sub(start).Seconds()

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

	var (
		convTimes  []float64 // yalnızca yakınsayan koşular
		commFracs  []float64
		interfs    []float64
		gains      []float64 // gain over greedy (tanımlı olanlar)
		poas       []float64 // empirik PoA (yalnızca optimum kanıtlananlar ve OPT>0)
		avgCaps    []float64
		fairs      []float64
		nashViols  []float64 // koşu başına sapmak isteyen istasyon sayısı (H-2 denetimi)
		nashOK     int       // 0 ihlalli (gerçek Nash dengesi) koşu sayısı
		zeroInterf int       // girişimi ~0 olan koşu sayısı ("0.0000" iddiasının dürüst hali)
		convCount  int
		optProven  int
		optZeroNE  int // optimum 0 iken NE>0 kalan koşular (PoA=+Inf örnekleri)
	)
	const eps = 1e-15

	for r := 0; r < runs; r++ {
		rng := rand.New(rand.NewSource(baseSeed + int64(r)))
		net := BuildNetwork(rng, SimN, SimAreaSize, SimThreshold, false)

		convSec, converged := RunSimulation(net, 20*time.Second)

		// Metrikler (NOT: CalculateShannonCapacity hâlâ Hata 4 & 5'i
		// içeriyor — FAILED istasyon şişirmesi ve zayıf SINR modeli.
		// Bu metriklerin mutlak değerleri o düzeltmelere kadar temkinli okunmalı.)
		total := 0.0
		for _, bs := range net {
			bs.CalculateShannonCapacity(net)
			total += bs.Throughput
		}
		avgCap := total / float64(len(net))
		fair := CalculateJainsFairness(net)
		obj := CalculateGlobalObjective(net)
		greedy := CalculateGreedyBaseline(net)
		opt := BruteForceOptimum(net, MaxColors, optBudget)

		// H-2 denetimi: nihai tahsis gerçekten Nash dengesi mi?
		viol, uncomm := VerifyNashEquilibrium(net)
		nashViols = append(nashViols, float64(viol))
		if viol == 0 && uncomm == 0 {
			nashOK++
		}

		commFrac := float64(CommittedCount(net)) / float64(len(net))
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

		fmt.Printf("Run %3d | conv %5.1fs (%v) | committed %3.0f%% | interf %.3e | gain %.3f | PoA>= %.3f (opt exact=%v) | NE viol: %d\n",
			r, convSec, converged, commFrac*100, obj, gain, poa, opt.Exact, viol)
	}

	fmt.Printf("\n================ ÖZET (%d koşu) ================\n", runs)
	fmt.Println("Format: ortalama ± %95 GA (1.96·σ/√n)")
	reportStat("Yakınsama süresi (s)", convTimes)
	reportStat("COMMITTED oranı", commFracs)
	reportStat("Toplam girişim (W)", interfs)
	reportStat("Gain over Greedy", gains)
	reportStat("Empirik PoA alt sınırı", poas)
	reportStat("Ort. kullanıcı hızı (Mbps)*", avgCaps)
	reportStat("Jain fairness*", fairs)
	reportStat("Nash ihlali (istasyon/koşu)", nashViols)
	fmt.Printf("\nYakınsayan koşu                 : %d/%d (%.0f%%)\n", convCount, runs, 100*float64(convCount)/float64(runs))
	fmt.Printf("Gerçek Nash dengesi olan koşu   : %d/%d (0 ihlal + 0 uncommitted)\n", nashOK, runs)
	fmt.Printf("Girişimi ~0 olan koşu           : %d/%d (%.0f%%)  <-- \"0.0000\" iddiasının dürüst hali\n", zeroInterf, runs, 100*float64(zeroInterf)/float64(runs))
	fmt.Printf("Optimum kanıtlanan koşu         : %d/%d\n", optProven, runs)
	if optZeroNE > 0 {
		fmt.Printf("OPT=0 iken NE>0 kalan koşu      : %d (PoA bu örneklerde sonsuz)\n", optZeroNE)
	}
	fmt.Println("\n* Hata 4 & 5 (throughput bug'ı, SINR modeli) düzeltilene kadar")
	fmt.Println("  kapasite/fairness değerleri temkinli yorumlanmalıdır.")
}
