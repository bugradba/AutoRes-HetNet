package main

import (
	"math"
	"math/rand"
	"testing"
)

// ============================================================
// ANALİTİK PHY TESTLERİ (Y-2 doğrulaması)
//
// Fizik katmanı artık deterministik olduğu için (tüm rastgelelik
// koşu başında dondurulur) kapasite, elle kurulmuş geometrilerde
// kapalı-form beklentiyle BİREBİR karşılaştırılabilir.
// ============================================================

// phyStation: elle yerleştirilmiş, gölgelemesiz (çarpan=1) istasyon.
func phyStation(id Agent_ID, x, y, ux, uy float64) *BaseStation {
	bs := NewBaseStation(id, x, y)
	bs.UserX, bs.UserY = ux, uy
	bs.ServingShadow = 1.0
	return bs
}

// TestCapacityNoInterference: girişimsiz, bilinen mesafede kapasite
// kapalı-form Shannon değerine eşit olmalı (tavanlar tetiklenmeden).
func TestCapacityNoInterference(t *testing.T) {
	d := 120.0 // tavanların altında kalacak kadar uzak
	bs := phyStation(0, 0, 0, d, 0)
	net := []*BaseStation{bs}
	colors := map[Agent_ID]PRB{0: 0}

	got := CapacityForColor(bs, 0, net, colors)

	sinr := TxPowerWatts * ReferenceLoss * math.Pow(d, -PathLossExponent) / NoiseWatts
	sinr = math.Min(sinr, SINRCapLinear)
	want := BandwidthHz * math.Min(math.Log2(1+sinr), SpectralEffCapBpsHz) / 1e6

	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("kapasite: got %.9f, want %.9f Mbps", got, want)
	}
}

// TestSpectralEfficiencyCap: çok yakın kullanıcıda kapasite tam olarak
// tavana (8 bps/Hz -> 20 MHz'te 160 Mbps) yapışmalı — asla üstüne çıkmamalı.
func TestSpectralEfficiencyCap(t *testing.T) {
	bs := phyStation(0, 0, 0, 1.0, 0) // 1 m: devasa SINR
	net := []*BaseStation{bs}
	colors := map[Agent_ID]PRB{0: 0}

	got := CapacityForColor(bs, 0, net, colors)
	want := BandwidthHz * SpectralEffCapBpsHz / 1e6 // 160 Mbps

	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("SE tavanı: got %.6f, want %.6f Mbps", got, want)
	}
}

// TestSINRCap: SINR 30 dB'de kırpılmalı. 30 dB tavan noktasının hemen
// altında/üstünde iki geometri kurup üsttekinin kırpıldığını doğrular.
func TestSINRCap(t *testing.T) {
	// SINR(d) = P*L*d^-a / N = 1000 olduğunda d = (P*L/(N*1000))^(1/a)
	dCap := math.Pow(TxPowerWatts*ReferenceLoss/(NoiseWatts*SINRCapLinear), 1.0/PathLossExponent)

	closer := phyStation(0, 0, 0, dCap*0.5, 0) // SINR >> 1000 -> kırpılır
	net := []*BaseStation{closer}
	colors := map[Agent_ID]PRB{0: 0}
	got := CapacityForColor(closer, 0, net, colors)
	want := BandwidthHz * math.Min(math.Log2(1+SINRCapLinear), SpectralEffCapBpsHz) / 1e6
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("SINR tavanı: got %.6f, want %.6f Mbps", got, want)
	}
}

// TestInterfererGeometry: girişim, BS->BS kenar ağırlığından DEĞİL,
// girişimci-BS -> kullanıcı mesafesinden hesaplanmalı (Y-2'nin özü).
func TestInterfererGeometry(t *testing.T) {
	// Servis eden BS orijinde, kullanıcısı (50, 0)'da.
	// Girişimci BS (80, 0)'da: BS->BS mesafesi 80 m ama
	// girişimci->KULLANICI mesafesi yalnızca 30 m.
	serving := phyStation(0, 0, 0, 50, 0)
	interferer := phyStation(1, 80, 0, 200, 200) // kendi kullanıcısı ilgisiz

	serving.Neighbros = []Agent_ID{1}
	serving.NeighborWeights[1] = 12345.0 // kasıtlı saçma kenar ağırlığı:
	// eski (hatalı) model bunu kullanırdı; yeni model kullanmamalı.
	serving.InterfShadow[1] = 1.0

	net := []*BaseStation{serving, interferer}
	colors := map[Agent_ID]PRB{0: 0, 1: 0} // eş-kanal

	got := CapacityForColor(serving, 0, net, colors)

	sig := TxPowerWatts * ReferenceLoss * math.Pow(50, -PathLossExponent)
	interf := TxPowerWatts * ReferenceLoss * math.Pow(30, -PathLossExponent) // 80-50=30 m
	sinr := math.Min(sig/(interf+NoiseWatts), SINRCapLinear)
	want := BandwidthHz * math.Min(math.Log2(1+sinr), SpectralEffCapBpsHz) / 1e6

	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("girişim geometrisi: got %.9f, want %.9f Mbps", got, want)
	}
}

// TestFailedStationZeroThroughput: renk atanmamış istasyon iletim
// yapamaz; throughput'u 0 olmalı (eski 'Hata 4' şişirmesinin testi).
func TestFailedStationZeroThroughput(t *testing.T) {
	bs := phyStation(0, 0, 0, 30, 0)
	net := []*BaseStation{bs}
	if got := CapacityForColor(bs, -1, net, map[Agent_ID]PRB{0: -1}); got != 0 {
		t.Fatalf("FAILED istasyon: got %.6f Mbps, want 0", got)
	}
}

// TestFrozenChannelReproducible: aynı seed ile kurulan iki ağ, aynı
// tahsis altında BİREBİR aynı throughput'u üretmeli ("seed-reproducible"
// iddiasının fizik katmanındaki testi; eski kod bunu geçemezdi).
func TestFrozenChannelReproducible(t *testing.T) {
	build := func() []float64 {
		rng := rand.New(rand.NewSource(123))
		net := BuildNetwork(rng, 25, SimAreaSize, SimThreshold, false)
		colors := FixedReuseAssignment(net) // deterministik tahsis
		return ThroughputsForAssignment(net, colors)
	}
	a, b := build(), build()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("istasyon %d: %.9f != %.9f (kanal donmamış!)", i, a[i], b[i])
		}
	}
}

// TestBaselinesAssignEveryStation: baseline şemaları her istasyona
// [0, K) aralığında geçerli bir renk atamalı.
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
// sayısına (3) eşitken DSATUR sıfır maliyetli (uygun) renklendirme bulmalı.
func TestDSATURProperColoringWhenPossible(t *testing.T) {
	origK := MaxColors
	defer func() { MaxColors = origK }()
	MaxColors = 3

	// Üçgen: 3 istasyon, hepsi komşu.
	a := phyStation(0, 0, 0, 10, 0)
	b := phyStation(1, 10, 0, 20, 0)
	c := phyStation(2, 5, 8, 15, 8)
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
	colors := DSATURAssignment(net)
	if cost := AssignmentCost(net, colors); cost != 0 {
		t.Fatalf("DSATUR üçgeni 3 renkle uygun boyayamadı, maliyet=%.3e", cost)
	}
}
