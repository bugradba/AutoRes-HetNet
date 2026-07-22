package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
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
				baseWeight := CouplingRefLoss * math.Pow(dist, -CouplingExponent)
				shadowing := math.Exp(rng.NormFloat64() * CouplingShadowLn)
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
	// G2: DONMUŞ KANAL GERÇEKLEŞMESİ (3GPP TR 38.901 UMa)
	// Kullanıcı konumları, LOS/NLOS durumları ve TÜM gölgeleme
	// çekilişleri koşu başında, deney rng'sinden BİR KEZ çizilir:
	//   - Aynı seed => aynı kanal => aynı throughput (tekrarlanabilir).
	//   - Kullanıcı konumu tahsisten bağımsızdır (cell shrinkage yok).
	//   - Tüm tahsis şemaları aynı gerçekleşme üzerinde karşılaştırılır.
	// Her bağlantının LOS durumu 38.901'in mesafeye bağlı LOS
	// olasılığıyla, gölgelemesi ise o duruma ait sigma ile (LOS 4 dB,
	// NLOS 6 dB) üretilir. Determinizm için Neighbros DİLİMİ üzerinde
	// yinelenir (harita yineleme sırası rastgele olurdu).
	// ============================================================
	for i := 0; i < n; i++ {
		bs := net[i]

		// 1) Kullanıcı konumu (tahsisten bağımsız, koşu boyunca sabit)
		angle := rng.Float64() * 2 * math.Pi
		userDist := UserMinDist + rng.Float64()*(UserMaxDist-UserMinDist)
		bs.UserX = bs.X + userDist*math.Cos(angle)
		bs.UserY = bs.Y + userDist*math.Sin(angle)

		// 2) Serving link: BS -> kendi kullanıcısı
		bs.ServingLOS = rng.Float64() < LOSProbabilityUMa(math.Max(userDist, 10.0))
		bs.ServingShadowDB = rng.NormFloat64() * ShadowSigmaDB(bs.ServingLOS)

		// 3) Girişim linkleri: her komşu BS -> BU kullanıcı
		for _, neighborID := range bs.Neighbros {
			interferer := net[int(neighborID)]
			d := dist2D(interferer.X, interferer.Y, bs.UserX, bs.UserY)
			los := rng.Float64() < LOSProbabilityUMa(math.Max(d, 10.0))
			bs.InterfLOS[neighborID] = los
			bs.InterfShadowDB[neighborID] = rng.NormFloat64() * ShadowSigmaDB(los)
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

// ------------------- EŞLEŞTİRİLMİŞ KARŞILAŞTIRMA -------------------
//
// Tüm tahsis şemaları AYNI koşuda, AYNI topoloji ve AYNI donmuş kanal
// üzerinde değerlendirildiği için gözlemler bağımsız değil, EŞLEŞTİRİLMİŞtir.
// Bu tasarımda doğru analiz, iki bağımsız ortalamayı kıyaslamak yerine
// koşu-başına FARKLARI incelemektir: topoloji şansından gelen varyans
// (ki baskın varyans kaynağıdır) farkta sadeleşir, aynı veriden çok daha
// dar güven aralığı ve çok daha yüksek istatistiksel güç elde edilir.

type PairedResult struct {
	N        int     // eşleşme sayısı
	MeanDiff float64 // ortalama fark (A - B)
	MedDiff  float64 // medyan fark (ağır kuyruklara dayanıklı)
	CI95     float64 // farkın %95 GA yarı genişliği
	RelPct   float64 // ortalama farkın B'ye oranı (%)
	T        float64 // eşleştirilmiş t istatistiği
	P        float64 // iki yönlü p-değeri
	ALower   int     // A'nın B'den KÜÇÜK olduğu koşu sayısı
	Ties     int     // beraberlik
}

// median: dizinin medyanı (girdiyi bozmaz).
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// percentile: doğrusal interpolasyonlu p-yüzdelik (p in [0,100]).
// 3GPP değerlendirmelerinde %5'lik kullanıcı hızı "hücre kenarı
// (cell-edge) throughput" olarak raporlanır.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	if len(c) == 1 {
		return c[0]
	}
	pos := p / 100 * float64(len(c)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return c[lo]
	}
	return c[lo] + (pos-float64(lo))*(c[hi]-c[lo])
}

// lnBeta: log B(a,b)
func lnBeta(a, b float64) float64 {
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	return la + lb - lab
}

// betacf: düzenli tamamlanmamış beta fonksiyonu için sürekli kesir
// (Lentz yöntemi). t-dağılımının kuyruk olasılığı için gerekir.
func betacf(a, b, x float64) float64 {
	const maxIter = 300
	const eps = 3e-14
	const fpmin = 1e-300

	qab, qap, qam := a+b, a+1, a-1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < fpmin {
		d = fpmin
	}
	d = 1 / d
	h := d

	for m := 1; m <= maxIter; m++ {
		fm := float64(m)
		m2 := 2 * fm

		aa := fm * (b - fm) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		h *= d * c

		aa = -(a + fm) * (qab + fm) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		del := d * c
		h *= del

		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}

// betaInc: düzenli tamamlanmamış beta I_x(a,b)
func betaInc(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	front := math.Exp(a*math.Log(x) + b*math.Log(1-x) - lnBeta(a, b))
	if x < (a+1)/(a+b+2) {
		return front * betacf(a, b, x) / a
	}
	return 1 - front*betacf(b, a, 1-x)/b
}

// tTestPValue: df serbestlik dereceli t dağılımında iki yönlü p-değeri.
// P(|T| > |t|) = I_{df/(df+t²)}(df/2, 1/2)
func tTestPValue(t float64, df int) float64 {
	if df <= 0 || math.IsNaN(t) {
		return math.NaN()
	}
	dfF := float64(df)
	return betaInc(dfF/2, 0.5, dfF/(dfF+t*t))
}

// PairedCompare: A ve B dizilerinin koşu-başına farkını analiz eder.
// Maliyet metriklerinde NEGATİF fark A'nın (dağıtık) daha iyi olduğunu,
// throughput metriklerinde POZİTİF fark A'nın daha iyi olduğunu gösterir.
func PairedCompare(a, b []float64) PairedResult {
	res := PairedResult{P: math.NaN(), T: math.NaN(), CI95: math.NaN()}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return res
	}

	diffs := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(a[i]) || math.IsNaN(b[i]) {
			continue
		}
		diffs = append(diffs, a[i]-b[i])
		switch {
		case a[i] < b[i]:
			res.ALower++
		case a[i] == b[i]:
			res.Ties++
		}
	}
	res.N = len(diffs)
	if res.N == 0 {
		return res
	}

	res.MeanDiff = mean(diffs)
	res.MedDiff = median(diffs)
	if mb := mean(b[:n]); mb != 0 {
		res.RelPct = 100 * res.MeanDiff / math.Abs(mb)
	}
	if res.N < 2 {
		return res
	}

	se := stdDev(diffs) / math.Sqrt(float64(res.N))
	res.CI95 = 1.96 * se
	if se > 0 {
		res.T = res.MeanDiff / se
		res.P = tTestPValue(res.T, res.N-1)
	} else if res.MeanDiff == 0 {
		res.T, res.P = 0, 1
	}
	return res
}

// sigMark: p-değerini okunur anlamlılık işaretine çevirir.
func sigMark(p float64) string {
	switch {
	case math.IsNaN(p):
		return "  -"
	case p < 0.001:
		return "***"
	case p < 0.01:
		return " **"
	case p < 0.05:
		return "  *"
	default:
		return " ns"
	}
}

// formatP: çok küçük p-değerlerini "<1e-12" biçiminde yazar.
func formatP(p float64) string {
	if math.IsNaN(p) {
		return "     -"
	}
	if p < 1e-12 {
		return "<1e-12"
	}
	return fmt.Sprintf("%.4f", p)
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
		edgeCaps   []float64 // %5'lik kullanıcı hızı (3GPP hücre kenarı)
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
		schemeEdge  = map[string][]float64{} // %5'lik hız = hücre kenarı (3GPP)
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
			schemeEdge[name] = append(schemeEdge[name], percentile(caps, 5))
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
		edgeCaps = append(edgeCaps, percentile(distCaps, 5))

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
	reportStat("Hücre kenarı hızı (%5, Mbps)*", edgeCaps)
	reportStat("Nash ihlali (istasyon/koşu)", nashViols)
	reportStat("Yakınsama (protokol turu)", convRounds)
	reportStat("Mesaj / istasyon", msgsPerBS)
	reportStat("Düşen mesaj / koşu", dropsTotal)
	reportStat("CONFLICT / koşu", conflicts)

	fmt.Println("\n---- BASELINE KARŞILAŞTIRMASI (aynı donmuş kanallar) ----")
	fmt.Printf("%-20s | %-22s | %-18s | %-18s | %s\n",
		"Şema", "Çakışma maliyeti (W)", "Ort. Mbps", "Hücre kenarı (%5)", "Jain")
	for _, name := range schemeNames {
		fmt.Printf("%-20s | %10.3e ± %.2e | %8.1f ± %5.1f | %8.1f ± %5.1f | %.3f ± %.3f\n",
			name,
			mean(schemeCosts[name]), ci95Half(schemeCosts[name]),
			mean(schemeMbps[name]), ci95Half(schemeMbps[name]),
			mean(schemeEdge[name]), ci95Half(schemeEdge[name]),
			mean(schemeJain[name]), ci95Half(schemeJain[name]))
	}
	// ---- EŞLEŞTİRİLMİŞ FARK ANALİZİ ----
	// Şemalar aynı koşuda aynı topoloji + aynı donmuş kanal üzerinde
	// değerlendirildiği için koşu-başına farkın analizi doğru testtir.
	const dist = "Distributed (NE)"
	fmt.Println("\n---- EŞLEŞTİRİLMİŞ FARK: Distributed (NE) − X (aynı koşu, aynı kanal) ----")
	fmt.Println("Maliyette NEGATİF fark, hızda POZİTİF fark dağıtık çözümün lehinedir.")
	fmt.Printf("\n%-20s | %-24s | %-8s | %-12s | %-10s | %s\n",
		"Çakışma maliyeti", "Δ ortalama (W)", "Δ %", "Δ medyan (W)", "p", "Dağıtık kazandı")
	for _, name := range schemeNames {
		if name == dist {
			continue
		}
		pc := PairedCompare(schemeCosts[dist], schemeCosts[name]) // düşük = iyi
		fmt.Printf("vs %-17s | %+10.3e ± %.2e | %+7.1f%% | %+11.3e | %s%s | %d/%d\n",
			name, pc.MeanDiff, pc.CI95, pc.RelPct, pc.MedDiff,
			formatP(pc.P), sigMark(pc.P), pc.ALower, pc.N)
	}

	fmt.Printf("\n%-20s | %-24s | %-8s | %-12s | %-10s | %s\n",
		"Kullanıcı hızı", "Δ ortalama (Mbps)", "Δ %", "Δ medyan", "p", "Dağıtık kazandı")
	for _, name := range schemeNames {
		if name == dist {
			continue
		}
		pm := PairedCompare(schemeMbps[dist], schemeMbps[name]) // yüksek = iyi
		wins := pm.N - pm.ALower - pm.Ties                      // hızda A>B => dağıtık kazanır
		fmt.Printf("vs %-17s | %+10.2f ± %8.2f | %+7.1f%% | %+11.2f | %s%s | %d/%d\n",
			name, pm.MeanDiff, pm.CI95, pm.RelPct, pm.MedDiff,
			formatP(pm.P), sigMark(pm.P), wins, pm.N)
	}
	fmt.Printf("\n%-20s | %-24s | %-8s | %-12s | %-10s | %s\n",
		"Hücre kenarı (%5)", "Δ ortalama (Mbps)", "Δ %", "Δ medyan", "p", "Dağıtık kazandı")
	for _, name := range schemeNames {
		if name == dist {
			continue
		}
		pe := PairedCompare(schemeEdge[dist], schemeEdge[name]) // yüksek = iyi
		wins := pe.N - pe.ALower - pe.Ties
		fmt.Printf("vs %-17s | %+10.2f ± %8.2f | %+7.1f%% | %+11.2f | %s%s | %d/%d\n",
			name, pe.MeanDiff, pe.CI95, pe.RelPct, pe.MedDiff,
			formatP(pe.P), sigMark(pe.P), wins, pe.N)
	}

	fmt.Println("\nAnlamlılık: *** p<0.001, ** p<0.01, * p<0.05, ns = anlamlı değil (eşleştirilmiş t-testi).")
	fmt.Println("Ortalama ile medyanın/kazanma oranının ayrışması, farkın birkaç aykırı koşudan")
	fmt.Println("geldiğini gösterir; böyle durumlarda medyan ve kazanma sayısı daha bilgilendiricidir.")

	fmt.Printf("\nYakınsayan koşu                 : %d/%d (%.1f%%)\n", convCount, runs, 100*float64(convCount)/float64(runs))
	fmt.Printf("Gerçek Nash dengesi olan koşu   : %d/%d (0 ihlal + 0 uncommitted)\n", nashOK, runs)
	fmt.Printf("Girişimi ~0 olan koşu           : %d/%d (%.1f%%)  <-- \"0.0000\" iddiasının dürüst hali\n", zeroInterf, runs, 100*float64(zeroInterf)/float64(runs))
	fmt.Printf("Optimum kanıtlanan koşu         : %d/%d\n", optProven, runs)
	if optZeroNE > 0 {
		fmt.Printf("OPT=0 iken NE>0 kalan koşu      : %d (PoA bu örneklerde sonsuz)\n", optZeroNE)
	}
	fmt.Println("\n* Kanal modeli: 3GPP TR 38.901 UMa (fc=3.5 GHz, hBS=25 m, hUT=1.5 m),")
	fmt.Println("  mesafeye bağlı LOS olasılığı, gölgeleme LOS 4 dB / NLOS 6 dB, donmuş kanal,")
	fmt.Println("  gerçek girişimci->UE geometrisi, N=-174+10log10(B)+7 dBm,")
	fmt.Println("  SINR<=30 dB ve spektral verim<=7.4 bps/Hz (20 MHz'te 148 Mbps tavanı).")
}
