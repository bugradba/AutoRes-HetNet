package main

import (
	"math"
	"math/rand"
	"testing"
)

// ============================================================
// G2 DOĞRULAMA: 3GPP TR 38.901 UMa ANALİTİK TESTLERİ
//
// Aşağıdaki beklenen değerler, koddan bağımsız olarak standardın
// kapalı-form denklemlerinden hesaplanmıştır (TR 38.901 v17,
// Tablo 7.4.1-1 ve 7.4.2-1). Bu testler, "kanal modeli 38.901 ile
// doğrulanmıştır" cümlesinin dayanağıdır.
//
// Ortak parametreler: fc = 3.5 GHz, hBS = 25 m, hUT = 1.5 m,
// B = 20 MHz, NF = 7 dB.
// ============================================================

func approxDB(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.4f, want %.4f (tol %g)", what, got, want, tol)
	}
}

// TestUMaBreakpointDistance: d'BP = 4·h'BS·h'UT·fc/c
// = 4 · 24 · 0.5 · 3.5e9 / 3e8 = 560 m
func TestUMaBreakpointDistance(t *testing.T) {
	approxDB(t, umaBreakpointDistance(), 560.0, 1e-6, "d'BP")
}

// TestUMaPathLossLOSAnalytic: d2D = 100 m, LOS, fc = 3.5 GHz.
//
//	d3D = sqrt(100² + 23.5²) = 102.7241 m
//	PL  = 28 + 22·log10(102.7241) + 20·log10(3.5) = 83.1382 dB
func TestUMaPathLossLOSAnalytic(t *testing.T) {
	approxDB(t, PathLossUMa(100, true), 83.1382, 1e-3, "PL_UMa-LOS(100 m)")

	// İkinci nokta: d2D = 50 m -> 77.2122 dB
	approxDB(t, PathLossUMa(50, true), 77.2122, 1e-3, "PL_UMa-LOS(50 m)")
}

// TestUMaPathLossNLOSAnalytic: aynı geometride NLOS.
//
//	PL' = 13.54 + 39.08·log10(102.7241) + 20·log10(3.5) − 0 = 103.0375 dB
//	PL  = max(83.1382, 103.0375) = 103.0375 dB
func TestUMaPathLossNLOSAnalytic(t *testing.T) {
	approxDB(t, PathLossUMa(100, false), 103.0375, 1e-3, "PL_UMa-NLOS(100 m)")
	approxDB(t, PathLossUMa(50, false), 92.5108, 1e-3, "PL_UMa-NLOS(50 m)")
}

// TestNLOSNeverBetterThanLOS: standardın max(...) kuralı — NLOS bir
// bağlantı hiçbir mesafede LOS'tan daha az kayıplı olamaz.
func TestNLOSNeverBetterThanLOS(t *testing.T) {
	for d := 10.0; d <= 1000.0; d += 5 {
		los, nlos := PathLossUMa(d, true), PathLossUMa(d, false)
		if nlos < los-1e-9 {
			t.Fatalf("d2D=%.0f m: NLOS (%.3f dB) < LOS (%.3f dB) — max() kuralı ihlal edildi", d, nlos, los)
		}
	}
}

// TestPathLossMonotonic: yol kaybı mesafeyle artmalı (her iki durumda).
func TestPathLossMonotonic(t *testing.T) {
	for _, los := range []bool{true, false} {
		prev := -math.MaxFloat64
		for d := 10.0; d <= 2000.0; d += 10 {
			pl := PathLossUMa(d, los)
			if pl < prev-1e-9 {
				t.Fatalf("LOS=%v, d2D=%.0f m: yol kaybı azaldı (%.3f < %.3f)", los, d, pl, prev)
			}
			prev = pl
		}
	}
}

// TestBreakpointContinuity: kırılma noktasında iki parçalı LOS modeli
// süreklidir (PL1(d'BP) == PL2(d'BP)).
func TestBreakpointContinuity(t *testing.T) {
	dBP := umaBreakpointDistance()
	before := PathLossUMa(dBP-0.001, true)
	after := PathLossUMa(dBP+0.001, true)
	if math.Abs(before-after) > 1e-3 {
		t.Fatalf("kırılma noktasında süreksizlik: %.6f vs %.6f dB", before, after)
	}
}

// TestLOSProbabilityAnalytic: TR 38.901 Tablo 7.4.2-1, UMa.
// hUT = 1.5 m <= 13 m olduğu için C'(hUT) = 0 ve düzeltme terimi 1'dir.
//
//	d2D <= 18 m -> 1
//	d2D = 50 m  -> 18/50 + e^(−50/63)·(1 − 18/50)  = 0.649402
//	d2D = 100 m -> 18/100 + e^(−100/63)·(1 − 0.18) = 0.347671
func TestLOSProbabilityAnalytic(t *testing.T) {
	approxDB(t, LOSProbabilityUMa(5), 1.0, 1e-12, "P_LOS(5 m)")
	approxDB(t, LOSProbabilityUMa(18), 1.0, 1e-12, "P_LOS(18 m)")
	approxDB(t, LOSProbabilityUMa(50), 0.649402, 1e-5, "P_LOS(50 m)")
	approxDB(t, LOSProbabilityUMa(100), 0.347671, 1e-5, "P_LOS(100 m)")

	// Olasılık [0,1] aralığında ve mesafeyle azalan olmalı
	prev := 1.0
	for d := 20.0; d <= 500.0; d += 10 {
		p := LOSProbabilityUMa(d)
		if p < 0 || p > 1 {
			t.Fatalf("d2D=%.0f m: P_LOS=%.4f aralık dışı", d, p)
		}
		if p > prev+1e-12 {
			t.Fatalf("d2D=%.0f m: P_LOS arttı (%.4f > %.4f)", d, p, prev)
		}
		prev = p
	}
}

// TestNoisePowerAnalytic: N = −174 + 10·log10(20e6) + 7 = −93.9897 dBm
// = 3.9905e-13 W. (Eski modeldeki elle konmuş 1e-13 W değil.)
func TestNoisePowerAnalytic(t *testing.T) {
	wantDBm := -93.98970
	gotDBm := 10 * math.Log10(NoisePowerWatts()*1000)
	approxDB(t, gotDBm, wantDBm, 1e-4, "gürültü gücü (dBm)")
	approxDB(t, NoisePowerWatts(), 3.990525e-13, 1e-18, "gürültü gücü (W)")
}

// --- SINR / KAPASİTE ZİNCİRİ ---

// phyStation: elle yerleştirilmiş, gölgelemesiz (0 dB) istasyon.
func phyStation(id Agent_ID, x, y, ux, uy float64, los bool) *BaseStation {
	bs := NewBaseStation(id, x, y)
	bs.UserX, bs.UserY = ux, uy
	bs.ServingLOS = los
	bs.ServingShadowDB = 0
	return bs
}

// TestCapacityNoInterferenceAnalytic: girişimsiz kapasite, uçtan uca
// kapalı-formla karşılaştırılır (38.901 PL -> SINR -> tavanlı Shannon).
func TestCapacityNoInterferenceAnalytic(t *testing.T) {
	const d = 100.0
	bs := phyStation(0, 0, 0, d, 0, true)
	net := []*BaseStation{bs}
	colors := map[Agent_ID]PRB{0: 0}

	got := CapacityForColor(bs, 0, net, colors)

	// Beklenen: PL = 83.1382 dB -> Rx = 46 − 83.1382 = −37.1382 dBm
	// N = −93.9897 dBm -> SNR = 56.85 dB -> 30 dB tavanına kırpılır
	// SE = min(log2(1+1000), 7.4) = 7.4 -> 20 MHz · 7.4 = 148 Mbps
	want := BandwidthHz * SpectralEffCapBpsHz / 1e6
	approxDB(t, got, want, 1e-9, "girişimsiz kapasite (Mbps)")
	if math.Abs(want-148.0) > 1e-9 {
		t.Fatalf("SE tavanı 20 MHz'te 148 Mbps olmalı, hesaplanan %.3f", want)
	}
}

// TestSpectralEfficiencyCapNeverExceeded: hiçbir geometride kapasite
// 148 Mbps'i (7.4 bps/Hz) aşmamalı — eski modelin 12.4 bps/Hz üreten
// hatası bir daha oluşamaz.
func TestSpectralEfficiencyCapNeverExceeded(t *testing.T) {
	maxCap := BandwidthHz * SpectralEffCapBpsHz / 1e6
	for d := 1.0; d <= 300.0; d += 1 {
		for _, los := range []bool{true, false} {
			bs := phyStation(0, 0, 0, d, 0, los)
			net := []*BaseStation{bs}
			c := CapacityForColor(bs, 0, net, map[Agent_ID]PRB{0: 0})
			if c > maxCap+1e-9 {
				t.Fatalf("d=%.0f LOS=%v: kapasite tavanı aşıldı (%.3f > %.3f Mbps)", d, los, c, maxCap)
			}
		}
	}
}

// TestSINRCapApplied: çok yakın kullanıcıda SINR tam olarak 30 dB'de
// kırpılmalı.
func TestSINRCapApplied(t *testing.T) {
	bs := phyStation(0, 0, 0, 10, 0, true) // model alt sınırı
	net := []*BaseStation{bs}
	sinr := SINRForColor(bs, 0, indexByID(net), map[Agent_ID]PRB{0: 0})
	approxDB(t, 10*math.Log10(sinr), SINRCapDB, 1e-9, "SINR tavanı (dB)")
}

// TestInterfererGeometryUsesUELocation: G2'nin özü — girişim, BS<->BS
// kenar ağırlığından DEĞİL, girişimci-BS -> KULLANICI mesafesinden
// hesaplanmalı. Kenar ağırlığına kasıtlı saçma bir değer konur;
// sonuç bu değerden etkilenmemelidir.
func TestInterfererGeometryUsesUELocation(t *testing.T) {
	// Servis eden BS orijinde, kullanıcısı (50, 0)'da.
	// Girişimci BS (80, 0)'da: BS<->BS mesafesi 80 m,
	// ama girişimci -> KULLANICI mesafesi yalnızca 30 m.
	serving := phyStation(0, 0, 0, 50, 0, true)
	interferer := phyStation(1, 80, 0, 200, 200, true)

	serving.Neighbros = []Agent_ID{1}
	serving.NeighborWeights[1] = 12345.0 // oyun grafı ağırlığı: PHY'ye girmemeli
	serving.InterfLOS[1] = true
	serving.InterfShadowDB[1] = 0

	net := []*BaseStation{serving, interferer}
	colors := map[Agent_ID]PRB{0: 0, 1: 0} // eş-kanal

	got := CapacityForColor(serving, 0, net, colors)

	// Kapalı form: 50 m serving, 30 m girişim, ikisi de LOS, gölgeleme 0
	sig := TxPowerWatts * math.Pow(10, -PathLossUMa(50, true)/10)
	interf := TxPowerWatts * math.Pow(10, -PathLossUMa(30, true)/10)
	sinr := math.Min(sig/(interf+NoisePowerWatts()), math.Pow(10, SINRCapDB/10))
	want := BandwidthHz * math.Min(math.Log2(1+sinr), SpectralEffCapBpsHz) / 1e6

	approxDB(t, got, want, 1e-9, "girişim geometrisi (Mbps)")

	// Ağırlığı 1000 kat değiştirmek sonucu DEĞİŞTİRMEMELİ
	serving.NeighborWeights[1] = 12345000.0
	if again := CapacityForColor(serving, 0, net, colors); math.Abs(again-got) > 1e-12 {
		t.Fatal("BS<->BS kenar ağırlığı SINR hesabına sızıyor (G2 ihlali)")
	}
}

// TestFailedStationZeroThroughput: renk atanmamış istasyon iletemez.
func TestFailedStationZeroThroughput(t *testing.T) {
	bs := phyStation(0, 0, 0, 30, 0, true)
	net := []*BaseStation{bs}
	if got := CapacityForColor(bs, -1, net, map[Agent_ID]PRB{0: -1}); got != 0 {
		t.Fatalf("FAILED istasyon: got %.6f Mbps, want 0", got)
	}
}

// TestFrozenChannelReproducible: aynı seed ile kurulan iki ağ, aynı
// tahsis altında BİREBİR aynı hızları üretmeli. Kanal çekilişleri
// time.Now() yerine koşu rng'sinden geldiği için bu artık garantidir.
func TestFrozenChannelReproducible(t *testing.T) {
	build := func() ([]float64, []bool) {
		rng := rand.New(rand.NewSource(123))
		net := BuildNetwork(rng, 25, SimAreaSize, SimThreshold, false)
		colors := FixedReuseAssignment(net) // deterministik tahsis
		los := make([]bool, len(net))
		for i, bs := range net {
			los[i] = bs.ServingLOS
		}
		return ThroughputsForAssignment(net, colors), los
	}
	a, losA := build()
	b, losB := build()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("istasyon %d: %.9f != %.9f (kanal donmamış!)", i, a[i], b[i])
		}
		if losA[i] != losB[i] {
			t.Fatalf("istasyon %d: LOS durumu tekrarlanmadı", i)
		}
	}
}

// TestFrozenChannelIndependentOfAllocation: aynı ağ üzerinde farklı
// tahsisler denendiğinde kanal (UE konumu, LOS, gölgeleme) değişmemeli.
// Eski modeldeki "cell shrinkage" bu testi geçemezdi.
func TestFrozenChannelIndependentOfAllocation(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	net := BuildNetwork(rng, 20, SimAreaSize, SimThreshold, false)

	snapshot := func() []float64 {
		s := make([]float64, 0, len(net)*3)
		for _, bs := range net {
			s = append(s, bs.UserX, bs.UserY, bs.ServingShadowDB)
		}
		return s
	}

	before := snapshot()
	_ = ThroughputsForAssignment(net, FixedReuseAssignment(net))
	_ = ThroughputsForAssignment(net, RandomAssignment(net, rng))
	_ = ThroughputsForAssignment(net, GreedyAssignment(net))
	after := snapshot()

	for i := range before {
		if before[i] != after[i] {
			t.Fatal("kanal gerçekleşmesi tahsise göre değişti (cell shrinkage geri geldi)")
		}
	}
}

// TestLOSStateAffectsCapacity: aynı geometride NLOS bağlantı, LOS'tan
// kesinlikle daha düşük (ya da tavanda eşit) hız vermelidir.
func TestLOSStateAffectsCapacity(t *testing.T) {
	// Tavanın bağlamayacağı kadar uzak bir mesafe seç
	const d = 250.0
	losBS := phyStation(0, 0, 0, d, 0, true)
	nlosBS := phyStation(0, 0, 0, d, 0, false)

	cLOS := CapacityForColor(losBS, 0, []*BaseStation{losBS}, map[Agent_ID]PRB{0: 0})
	cNLOS := CapacityForColor(nlosBS, 0, []*BaseStation{nlosBS}, map[Agent_ID]PRB{0: 0})

	if !(cNLOS < cLOS) {
		t.Fatalf("NLOS (%.2f Mbps) LOS'tan (%.2f Mbps) düşük olmalıydı", cNLOS, cLOS)
	}
}

// TestBaselinesAssignEveryStation: baseline şemaları her istasyona
// [0, K) aralığında geçerli renk atamalı.
func TestBaselinesAssignEveryStation(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)

	schemes := map[string]map[Agent_ID]PRB{
		"greedy": GreedyAssignment(net),
		"dsatur": DSATURAssignment(net),
		"fixed":  FixedReuseAssignment(net),
		"random": RandomAssignment(net, rng),
	}
	for name, colors := range schemes {
		if len(colors) != len(net) {
			t.Fatalf("%s: %d/%d istasyon renklendi", name, len(colors), len(net))
		}
		for id, c := range colors {
			if c < 0 || c >= MaxColors {
				t.Fatalf("%s: BS-%d geçersiz renk %d", name, id, c)
			}
		}
	}
}

// TestDSATURProperColoringWhenPossible: K, üçgen grafın kromatik
// sayısına (3) eşitken DSATUR sıfır maliyetli renklendirme bulmalı.
func TestDSATURProperColoringWhenPossible(t *testing.T) {
	origK := MaxColors
	defer func() { MaxColors = origK }()
	MaxColors = 3

	a := phyStation(0, 0, 0, 10, 0, true)
	b := phyStation(1, 10, 0, 20, 0, true)
	c := phyStation(2, 5, 8, 15, 8, true)
	link := func(x, y *BaseStation) {
		x.NeighborWeights[y.ID] = 1e-9
		y.NeighborWeights[x.ID] = 1e-9
		x.Neighbros = append(x.Neighbros, y.ID)
		y.Neighbros = append(y.Neighbros, x.ID)
	}
	link(a, b)
	link(b, c)
	link(a, c)

	net := []*BaseStation{a, b, c}
	if cost := AssignmentCost(net, DSATURAssignment(net)); cost != 0 {
		t.Fatalf("DSATUR üçgeni 3 renkle uygun boyayamadı, maliyet=%.3e", cost)
	}
}
