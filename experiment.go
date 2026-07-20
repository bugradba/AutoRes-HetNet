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
	// ============================================================
	// Y-2: DONMUŞ KANAL GERÇEKLEŞMESİ (frozen channel realization)
	// Kullanıcı konumları ve TÜM PHY gölgeleme çekilişleri koşu
	// başında, deney rng'sinden BİR KEZ çizilir:
	//   - Aynı seed => aynı kanal => aynı throughput (tekrarlanabilir).
	//   - Kullanıcı konumu tahsisten bağımsızdır (cell shrinkage yok).
	//   - Tüm tahsis şemaları aynı gerçekleşme üzerinde karşılaştırılır.
	// Determinizm için Neighbros DİLİMİ üzerinde yinelenir (harita
	// yineleme sırası rastgele olurdu).
	// ============================================================
	for i := 0; i < n; i++ {
		bs := net[i]
		angle := rng.Float64() * 2 * math.Pi
		userDist := UserMinDist + rng.Float64()*(UserMaxDist-UserMinDist)
		bs.UserX = bs.X + userDist*math.Cos(angle)
		bs.UserY = bs.Y + userDist*math.Sin(angle)
		bs.ServingShadow = math.Exp(rng.NormFloat64() * ShadowSigmaLn)
		for _, neighborID := range bs.Neighbros {
			bs.InterfShadow[neighborID] = math.Exp(rng.NormFloat64() * ShadowSigmaLn)
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
	// Pencere (types.go/QuietWindow), COMMITTED denetim periyodundan
	// (ThinkPeriod) ve commit zaman aşımından uzun olmalı ki "sessizlik"
	// gerçekten "kimsenin sapma isteği kalmadı" anlamına gelsin.
	// -timescale ile diğer tüm sürelerle birlikte ölçeklenir.
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
		time.Sleep(PollInterval) // yoklama aralığı
		cur := takeSnapshot(net)
		if !snapshotsEqual(cur, prev) {
			prev = cur
			lastChange = time.Now()
		}
		if allCommittedSnap(cur) && time.Since(lastChange) >= QuietWindow {
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
		time.Sleep(CommitTimeout + 200*time.Millisecond)
	}
	return convSec, converged
}

// ------------------- MESAJ İSTATİSTİKLERİ (Y-4) -------------------

type MessageStats struct {
	Sent      int64
	Dropped   int64
	Conflicts int64
}

// CollectMessageStats: koşu sonunda (Stop + wg.Wait sonrası) tüm
// istasyonların sayaçlarını toplar.
func CollectMessageStats(net []*BaseStation) MessageStats {
	var st MessageStats
	for _, bs := range net {
		st.Sent += bs.MsgSent.Load()
		st.Dropped += bs.MsgDropped.Load()
		st.Conflicts += bs.ConflictsSent.Load()
	}
	return st
}

// meanOfServed: renk atanmış istasyonların ortalama throughput'u.
// (FAILED istasyonların 0 Mbps'i "servis edilen kullanıcı" ortalamasını
// sulandırmasın; kaç istasyonun servis dışı kaldığı ayrıca raporlanır.)
func meanOfServed(caps []float64, colors map[Agent_ID]PRB, net []*BaseStation) float64 {
	sum, n := 0.0, 0
	for i, bs := range net {
		if c, ok := colors[bs.ID]; ok && c != -1 {
			sum += caps[i]
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func jainOf(caps []float64) float64 {
	var s, ss float64
	for _, x := range caps {
		s += x
		ss += x * x
	}
	if ss == 0 {
		return 0
	}
	return s * s / (float64(len(caps)) * ss)
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
		convRounds []float64 // yakınsama süresi, protokol turu (think-period) cinsinden
		msgsPerBS  []float64 // istasyon başına ortalama mesaj (Y-4)
		dropsTotal []float64 // koşu başına düşen mesaj sayısı (Y-4)
		conflicts  []float64 // koşu başına CONFLICT sayısı

		// Baseline karşılaştırması (Y-1): tüm şemalar AYNI donmuş kanal
		// üzerinde değerlendirilir; satırlar şema, metrikler maliyet /
		// servis edilen kullanıcı başına Mbps / Jain.
		schemeCosts = map[string][]float64{}
		schemeMbps  = map[string][]float64{}
		schemeJain  = map[string][]float64{}
		schemeNames = []string{"Distributed (NE)", "Centralized greedy", "DSATUR", "Fixed reuse", "Random"}
		zeroInterf  int // girişimi ~0 olan koşu sayısı ("0.0000" iddiasının dürüst hali)
		convCount   int
		optProven   int
		optZeroNE   int // optimum 0 iken NE>0 kalan koşular (PoA=+Inf örnekleri)
	)
	const eps = 1e-15

	for r := 0; r < runs; r++ {
		rng := rand.New(rand.NewSource(baseSeed + int64(r)))
		net := BuildNetwork(rng, SimN, SimAreaSize, SimThreshold, false)

		convSec, converged := RunSimulation(net, MaxWait)

		// Metrikler — Y-2 sonrası: donmuş kanal, girişimci->kullanıcı
		// geometrisi, SINR/SE tavanları; FAILED istasyon 0 Mbps sayılır.
		distColors := ColorsOfNetwork(net)
		distCaps := ThroughputsForAssignment(net, distColors)
		for i, bs := range net {
			bs.Throughput = distCaps[i]
		}
		avgCap := meanOfServed(distCaps, distColors, net)
		fair := jainOf(distCaps)
		obj := CalculateGlobalObjective(net)
		greedy := CalculateGreedyBaseline(net)

		// Y-1: baseline şemaları — aynı topoloji + aynı donmuş kanal.
		// Random, deney rng'sinden beslenir (seed-tekrarlanabilir).
		assignments := map[string]map[Agent_ID]PRB{
			"Distributed (NE)":   distColors,
			"Centralized greedy": GreedyAssignment(net),
			"DSATUR":             DSATURAssignment(net),
			"Fixed reuse":        FixedReuseAssignment(net),
			"Random":             RandomAssignment(net, rng),
		}
		for name, colors := range assignments {
			caps := ThroughputsForAssignment(net, colors)
			schemeCosts[name] = append(schemeCosts[name], AssignmentCost(net, colors))
			schemeMbps[name] = append(schemeMbps[name], meanOfServed(caps, colors, net))
			schemeJain[name] = append(schemeJain[name], jainOf(caps))
		}

		// Y-4: mesaj istatistikleri
		mst := CollectMessageStats(net)
		msgsPerBS = append(msgsPerBS, float64(mst.Sent)/float64(len(net)))
		dropsTotal = append(dropsTotal, float64(mst.Dropped))
		conflicts = append(conflicts, float64(mst.Conflicts))
		opt := BruteForceOptimum(net, int(MaxColors), optBudget)

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
			// O-3/README: zaman ölçeğinden bağımsız birim — protokol turu
			convRounds = append(convRounds, convSec/ThinkPeriod.Seconds())
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
	reportStat("Yakınsama (protokol turu)", convRounds)
	reportStat("Mesaj / istasyon", msgsPerBS)
	reportStat("Düşen mesaj / koşu", dropsTotal)
	reportStat("CONFLICT / koşu", conflicts)

	fmt.Println("\n---- BASELINE KARŞILAŞTIRMASI (aynı donmuş kanallar) ----")
	fmt.Printf("%-20s | %-22s | %-18s | %s\n", "Şema", "Çakışma maliyeti (W)", "Mbps / servis edilen", "Jain")
	for _, name := range schemeNames {
		fmt.Printf("%-20s | %10.3e ± %.2e | %8.1f ± %5.1f | %.3f ± %.3f\n",
			name,
			mean(schemeCosts[name]), ci95Half(schemeCosts[name]),
			mean(schemeMbps[name]), ci95Half(schemeMbps[name]),
			mean(schemeJain[name]), ci95Half(schemeJain[name]))
	}
	fmt.Printf("\nYakınsayan koşu                 : %d/%d (%.0f%%)\n", convCount, runs, 100*float64(convCount)/float64(runs))
	fmt.Printf("Gerçek Nash dengesi olan koşu   : %d/%d (0 ihlal + 0 uncommitted)\n", nashOK, runs)
	fmt.Printf("Girişimi ~0 olan koşu           : %d/%d (%.0f%%)  <-- \"0.0000\" iddiasının dürüst hali\n", zeroInterf, runs, 100*float64(zeroInterf)/float64(runs))
	fmt.Printf("Optimum kanıtlanan koşu         : %d/%d\n", optProven, runs)
	if optZeroNE > 0 {
		fmt.Printf("OPT=0 iken NE>0 kalan koşu      : %d (PoA bu örneklerde sonsuz)\n", optZeroNE)
	}
	fmt.Println("\n* Kapasite/fairness: donmuş kanal, girişimci->kullanıcı geometrisi,")
	fmt.Println("  SINR<=30dB ve <=8 bps/Hz tavanlarıyla (Y-2 düzeltmesi sonrası model).")
}
