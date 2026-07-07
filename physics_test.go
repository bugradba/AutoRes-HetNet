package main

import (
	"math"
	"testing"
)

// Tavan kapasitesi: B · SE_max = 20e6 · 8 / 1e6 = 160 Mbps
const maxCapMbps = BandwidthHz * MaxSpectralEff / 1e6

// makePair: (0,0) ve (x2,0) konumlarında komşu iki istasyon.
func makePair(x2 float64) []*BaseStation {
	a := NewBaseStation(0, 0, 0)
	b := NewBaseStation(1, x2, 0)
	w := 1e-9
	a.NeighborWeights[b.ID] = w
	b.NeighborWeights[a.ID] = w
	a.Neighbros = []Agent_ID{b.ID}
	b.Neighbros = []Agent_ID{a.ID}
	return []*BaseStation{a, b}
}

// makeChannel: gölgeleme = 1, UE'ler kendi BS'lerinden +x yönünde dServ uzakta.
// Deterministik kanal => formül analitik değerlerle doğrulanabilir.
func makeChannel(net []*BaseStation, dServ float64) *Channel {
	n := len(net)
	ch := &Channel{
		UEX: make([]float64, n), UEY: make([]float64, n),
		DServ:        make([]float64, n),
		ServShadow:   make([]float64, n),
		InterfShadow: make([]map[Agent_ID]float64, n),
	}
	for i, bs := range net {
		ch.DServ[i] = dServ
		ch.UEX[i] = bs.X + dServ
		ch.UEY[i] = bs.Y
		ch.ServShadow[i] = 1.0
		ch.InterfShadow[i] = map[Agent_ID]float64{}
		for nid := range bs.NeighborWeights {
			ch.InterfShadow[i][nid] = 1.0
		}
	}
	return ch
}

// HATA 4 (1/2): hizmette olmayan (FAILED) istasyon 0 Mbps.
func TestFailedStationHasZeroThroughput(t *testing.T) {
	net := makePair(50)
	ch := makeChannel(net, 50)
	thr := ComputeThroughputs(net, []PRB{2, 3}, []bool{false, true}, ch)
	if thr[0] != 0 {
		t.Errorf("FAILED istasyon %.2f Mbps raporladı; 0 olmalıydı", thr[0])
	}
}

// HATA 5.3: Girişimsiz linkte SNR = Ptx·G0·d^-α / N0B = 3.2e-8/1e-13 = 3.2e5
// => SINR tavanı (1e3) bağlar => SE = min(log2(1001), 8) = 8 => tam 160 Mbps.
func TestSpectralEfficiencyCapExact(t *testing.T) {
	net := makePair(50)
	ch := makeChannel(net, 50)
	thr := ComputeThroughputs(net, []PRB{2, 3}, []bool{true, true}, ch) // farklı renk
	if math.Abs(thr[0]-maxCapMbps) > 1e-9 {
		t.Errorf("girişimsiz kapasite %.6f; tam %.1f beklenirdi (tavan)", thr[0], maxCapMbps)
	}
}

// HATA 5.1 + analitik doğrulama: iki BS aynı noktada (x2=0), aynı renk.
// dJU = dServ => I = S => SINR = S/(S+N) ≈ 1 => SE = log2(2) = 1 => 20 Mbps.
func TestColocatedInterfererGivesOneBpsHz(t *testing.T) {
	net := makePair(0) // b, a ile aynı noktada
	ch := makeChannel(net, 50)
	thr := ComputeThroughputs(net, []PRB{2, 2}, []bool{true, true}, ch)
	want := BandwidthHz * 1.0 / 1e6 // 20 Mbps
	if math.Abs(thr[0]-want) > 0.01 {
		t.Errorf("dipteki girişimciyle kapasite %.4f; ~%.1f Mbps beklenirdi (SINR≈1)", thr[0], want)
	}
}

// Girişimcinin UZAKLIĞI önemli (Hata 5.1'in özü): girişimci UE'den
// uzaklaştıkça kapasite tekdüze artmalı (aynı donmuş kanalda).
func TestInterferenceDecaysWithInterfererDistance(t *testing.T) {
	prev := -1.0
	// UE, a'nın +x yönünde x=50'de. x2 seçimleri interferer->UE mesafesini
	// kesin artan yapar: dJU = 10, 100, 200, 400 m.
	for _, x2 := range []float64{60, 150, 250, 450} {
		net := makePair(x2)
		ch := makeChannel(net, 50) // UE, a'nın +x yönünde 50 m: x2 büyüdükçe interferer uzaklaşır
		thr := ComputeThroughputs(net, []PRB{2, 2}, []bool{true, true}, ch)
		if thr[0] <= prev {
			t.Fatalf("girişimci uzaklaştıkça kapasite artmadı: x2=%.0f -> %.4f (önceki %.4f)", x2, thr[0], prev)
		}
		prev = thr[0]
	}
}

// HATA 4 (2/2): FAILED komşu yayın yapamaz => girişim üretmemeli.
// Aynı noktada, aynı renk 'istemiş' ama commit edememiş komşu: kapasite tavanda kalmalı.
func TestFailedNeighborProducesNoInterference(t *testing.T) {
	net := makePair(0)
	net[1].NeighborMap[net[0].ID] = 2 // eski kodun zehirleyicisi (artık okunmuyor)
	ch := makeChannel(net, 50)
	thr := ComputeThroughputs(net, []PRB{2, 2}, []bool{true, false}, ch)
	if math.Abs(thr[0]-maxCapMbps) > 1e-9 {
		t.Errorf("FAILED komşu girişim üretmiş: %.4f Mbps (160 beklenirdi)", thr[0])
	}
}

// HATA 6: Donmuş kanalda TAHSİS farkı sonuca yansımalı — aynı kanal
// üzerinde çakışmasız atama, tam-çakışmalı atamadan kesin iyi olmalı.
func TestFrozenChannelIsolatesAllocationEffect(t *testing.T) {
	net := makePair(30)
	ch := makeChannel(net, 50)
	served := []bool{true, true}
	clash := ComputeThroughputs(net, []PRB{1, 1}, served, ch)
	clean := ComputeThroughputs(net, []PRB{1, 2}, served, ch)
	if !(clean[0] > clash[0] && clean[1] > clash[1]) {
		t.Errorf("aynı kanalda çakışmasız atama üstün olmalıydı: clean=%v clash=%v", clean, clash)
	}
}
