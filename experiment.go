package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// HATA 3 DÜZELTMESİ: Monte Carlo + güven aralığı.
// Tek bir stokastik koşu temsil edici değildir; koşu, duvar saatiyle
// değil MANTIKSAL yakınsamayla ("tüm istasyonlar COMMITTED") biter.
// HATA 6 DÜZELTMESİ: her koşuda kanal bir kez çekilir; dağıtık NE ve
// tüm baseline şemalar AYNI donmuş gerçekleme üzerinde karşılaştırılır.
// ============================================================

// BuildNetwork: tohumlu rng ile TEKRARLANABİLİR topoloji kurar.
func BuildNetwork(rng *rand.Rand, n int, areaSize, threshold float64, verbose bool) []*BaseStation {
	net := make([]*BaseStation, n)
	for i := 0; i < n; i++ {
		net[i] = NewBaseStation(Agent_ID(i), rng.Float64()*areaSize, rng.Float64()*areaSize)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dist := Distance(net[i], net[j])
			if dist < threshold {
				baseWeight := ReferenceLoss * math.Pow(dist, -PathLossExponent)
				shadowing := math.Exp(rng.NormFloat64() * ShadowSigma)
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

// AllCommitted: tüm istasyonlar COMMITTED mi? (Mutex ile güvenli okuma.)
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

	// Yoklama aralığı zaman ölçeğiyle uyumlu (ThinkPeriod/10, min 1 ms).
	poll := ThinkPeriod / 10
	if poll < time.Millisecond {
		poll = time.Millisecond
	}

	deadline := start.Add(maxWait)
	for {
		time.Sleep(poll)
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

	// Yakınsamadan bittiyse: bekleyen commit zamanlayıcıları hâlâ
	// çalışıyor olabilir; metrikleri yarışsız okumak için süre tanı.
	if !converged {
		time.Sleep(CommitTimeout + 200*time.Millisecond)
	}
	return convSec, converged
}

// ------------------- MESAJ İSTATİSTİKLERİ -------------------

// MessageStats: bir koşunun toplam mesaj sayaçları.
// Total = kuyruğa giren tüm mesajlar + düşenler.
type MessageStats struct {
	Hellos, Proposes, Successes, Conflicts int64
	Dropped                                int64
	Total                                  int64
}

func CollectMessageStats(net []*BaseStation) MessageStats {
	var ms MessageStats
	for _, bs := range net {
		ms.Hellos += atomic.LoadInt64(&bs.MsgSent[MSG_HELLO])
		ms.Proposes += atomic.LoadInt64(&bs.MsgSent[MSG_PROPOSE])
		ms.Successes += atomic.LoadInt64(&bs.MsgSent[MSG_SUCCESS])
		ms.Conflicts += atomic.LoadInt64(&bs.MsgSent[MSG_CONFLICT])
		ms.Dropped += atomic.LoadInt64(&bs.MsgDropped)
	}
	ms.Total = ms.Hellos + ms.Proposes + ms.Successes + ms.Conflicts + ms.Dropped
	return ms
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
		return 0 // tek örneklemde GA tanımsız; NaN yerine 0 bas (kozmetik)
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

// meanServed: yalnızca hizmetteki (served) istasyonlar üzerinden ortalama.
// HATA 4: FAILED istasyon 0 Mbps'tir ve paydaya GİRMEZ.
func meanServed(thr []float64, served []bool) float64 {
	s, n := 0.0, 0
	for i, x := range thr {
		if served[i] {
			s += x
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return s / float64(n)
}

// ------------------- MONTE CARLO ÇEKİRDEĞİ -------------------

type schemeAgg struct {
	cost, thr, fair []float64
}

// RunMonteCarlo: 'runs' adet bağımsız koşu. Her koşuda seed = baseSeed + r
// => topoloji ve kanal tekrarlanabilir. (Asenkron mesaj yarışları doğası
// gereği deterministik değildir; bu, ölçmek İSTEDİĞİMİZ stokastikliğin
// bir parçasıdır.)
func RunMonteCarlo(runs int, baseSeed int64, optBudget time.Duration) {
	fmt.Printf("--- MONTE CARLO: %d koşu (baseSeed=%d, N=%d, Alan=%.0fm, Eşik=%.0fm, K=%d) ---\n\n",
		runs, baseSeed, SimN, SimAreaSize, SimThreshold, MaxColors)

	schemeNames := []string{"Dağıtık (NE)", "CFL (yayın)", "Greedy", "DSATUR", "Sabit reuse", "Rastgele"}
	cmp := map[string]*schemeAgg{}
	for _, n := range schemeNames {
		cmp[n] = &schemeAgg{}
	}

	var (
		convRounds []float64 // yalnızca yakınsayan koşular (protokol turu)
		commFracs  []float64
		interfs    []float64
		gains      []float64 // gain over greedy (tanımlı olanlar)
		poas       []float64 // empirik PoA alt sınırı
		msgPerBS   []float64
		drops      []float64
		avgCaps    []float64
		fairs      []float64
		zeroInterf int
		convCount  int

		cflRoundsAll []float64 // yalnızca yakınsayan CFL koşuları
		cflConvCount int
		optProven    int
		optZeroNE    int // OPT=0 iken NE>0 kalan koşular (PoA=+Inf örnekleri)
	)
	const eps = 1e-15

	for r := 0; r < runs; r++ {
		rng := rand.New(rand.NewSource(baseSeed + int64(r)))
		net := BuildNetwork(rng, SimN, SimAreaSize, SimThreshold, false)

		convSec, converged := RunSimulation(net, 40*ThinkPeriod)
		convR := convSec / ThinkPeriod.Seconds()

		// HATA 6: kanal bir kez çekilir; beş şema aynı gerçeklemede.
		ch := DrawChannel(net, rng)
		neAssign, neServed := NEAssignment(net)
		thrNE := ComputeThroughputs(net, neAssign, neServed, ch)
		for i, bs := range net {
			bs.Throughput = thrNE[i] // tekil koşu uyumluluğu
		}

		allServed := make([]bool, len(net))
		for i := range allServed {
			allServed[i] = true
		}

		// Yayımlanmış dağıtık referans: CFL (Leith & Clifford 2006;
		// Duffy ve ark. 2008). Senkron, mesajsız; aynı topoloji ve aynı
		// donmuş kanal üzerinde değerlendirilir. rng'den beslenir =>
		// aynı seed'de makineden bağımsız birebir tekrarlanabilir.
		cflAssign, cflR, cflOK := RunCFL(net, MaxColors, CFLDefaultB, CFLMaxRounds, rng)
		if cflOK {
			cflConvCount++
			cflRoundsAll = append(cflRoundsAll, float64(cflR))
		}

		schemes := []struct {
			name   string
			assign []PRB
			served []bool
			thr    []float64
		}{
			{"Dağıtık (NE)", neAssign, neServed, thrNE},
			{"CFL (yayın)", cflAssign, allServed, nil},
			{"Greedy", GreedyAssignment(net), allServed, nil},
			{"DSATUR", DSATURAssignment(net), allServed, nil},
			{"Sabit reuse", FixedReuseAssignment(net, MaxColors), allServed, nil},
			{"Rastgele", RandomAssignment(net, MaxColors, rng), allServed, nil},
		}
		for _, sc := range schemes {
			thr := sc.thr
			if thr == nil {
				thr = ComputeThroughputs(net, sc.assign, sc.served, ch)
			}
			cmp[sc.name].cost = append(cmp[sc.name].cost, AssignmentCost(net, sc.assign))
			cmp[sc.name].thr = append(cmp[sc.name].thr, meanServed(thr, sc.served))
			cmp[sc.name].fair = append(cmp[sc.name].fair, JainOf(thr))
		}

		obj := AssignmentCost(net, neAssign)
		greedy := cmp["Greedy"].cost[len(cmp["Greedy"].cost)-1]
		opt := BruteForceOptimum(net, MaxColors, optBudget)

		ms := CollectMessageStats(net)
		msgPerBS = append(msgPerBS, float64(ms.Total)/float64(len(net)))
		drops = append(drops, float64(ms.Dropped))

		commFrac := float64(CommittedCount(net)) / float64(len(net))
		commFracs = append(commFracs, commFrac)
		interfs = append(interfs, obj)
		avgCaps = append(avgCaps, meanServed(thrNE, neServed))
		fairs = append(fairs, JainOf(thrNE))

		if converged {
			convCount++
			convRounds = append(convRounds, convR)
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

		fmt.Printf("Run %3d | conv %5.1f tur (%v) | committed %3.0f%% | interf %.3e | msg/BS %5.1f | gain %.3f | PoA>= %.3f (opt exact=%v)\n",
			r, convR, converged, commFrac*100, obj, float64(ms.Total)/float64(len(net)), gain, poa, opt.Exact)
	}

	fmt.Printf("\n================ ÖZET (%d koşu) ================\n", runs)
	fmt.Println("Format: ortalama ± %95 GA (1.96·σ/√n)")
	reportStat("Yakınsama (protokol turu)", convRounds)
	reportStat("COMMITTED oranı", commFracs)
	reportStat("Toplam girişim (W)", interfs)
	reportStat("Gain over Greedy", gains)
	reportStat("Empirik PoA alt sınırı", poas)
	reportStat("Mesaj / istasyon", msgPerBS)
	reportStat("Düşen mesaj / koşu", drops)
	reportStat("Ort. hız (Mbps, served)", avgCaps)
	reportStat("Jain fairness (NE)", fairs)
	reportStat("CFL turu (yakınsayan)", cflRoundsAll)
	fmt.Printf("\nYakınsayan koşu                 : %d/%d (%.0f%%)\n", convCount, runs, 100*float64(convCount)/float64(runs))
	fmt.Printf("CFL yakınsayan koşu             : %d/%d (%.0f%%) [tavan %d tur]\n", cflConvCount, runs, 100*float64(cflConvCount)/float64(runs), CFLMaxRounds)
	fmt.Printf("Girişimi ~0 olan koşu           : %d/%d (%.0f%%)\n", zeroInterf, runs, 100*float64(zeroInterf)/float64(runs))
	fmt.Printf("Optimum kanıtlanan koşu         : %d/%d\n", optProven, runs)
	if optZeroNE > 0 {
		fmt.Printf("OPT=0 iken NE>0 kalan koşu      : %d (PoA bu örneklerde sonsuz)\n", optZeroNE)
	}

	// HATA 6: donmuş kanal üzerinde şema karşılaştırma tablosu.
	fmt.Println("\n--- ŞEMA KARŞILAŞTIRMASI (aynı topoloji + aynı donmuş kanal) ---")
	fmt.Printf("%-14s | %-22s | %-18s | %s\n", "Şema", "Çakışma maliyeti", "Mbps / served", "Jain")
	for _, name := range schemeNames {
		a := cmp[name]
		fmt.Printf("%-14s | %10.3e ± %8.2e | %7.1f ± %6.1f | %.3f ± %.3f\n",
			name, mean(a.cost), ci95Half(a.cost), mean(a.thr), ci95Half(a.thr), mean(a.fair), ci95Half(a.fair))
	}
}
