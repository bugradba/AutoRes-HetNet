package main

import (
	"math"
	"math/rand"
	"testing"
)

// ============================================================
// A3 DOĞRULAMA: HETEROJEN AĞ (HetNet)
//
// Ağ artık homojen makro istasyonlardan değil, makro (40 W/25 m) +
// piko (1 W/10 m) karışımından oluşur. Bu testler istasyon tiplerinin
// doğru atandığını ve fizik katmanının istasyon-başına gücü/yüksekliği
// kullandığını garanti eder.
// ============================================================

// TestHetNetHasBothTypes: PicoFraction=0.5 ile ağ hem makro hem piko
// istasyon içermeli (homojen değil).
func TestHetNetHasBothTypes(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)

	macro, pico := 0, 0
	for _, bs := range net {
		if bs.IsPico {
			pico++
			if bs.TxWatts != PicoTxWatts || bs.HeightM != PicoHeightM {
				t.Fatalf("piko BS-%d yanlış parametre: %.1f W, %.1f m", bs.ID, bs.TxWatts, bs.HeightM)
			}
		} else {
			macro++
			if bs.TxWatts != MacroTxWatts || bs.HeightM != MacroHeightM {
				t.Fatalf("makro BS-%d yanlış parametre: %.1f W, %.1f m", bs.ID, bs.TxWatts, bs.HeightM)
			}
		}
	}
	if macro == 0 || pico == 0 {
		t.Fatalf("HetNet her iki tipi içermeli: %d makro, %d piko", macro, pico)
	}
}

// TestPicoLowerPowerThanMacro: aynı geometride bir piko istasyon, bir
// makrodan daha düşük sinyal gücü (dolayısıyla daha düşük SINR) üretmeli.
// SINR'ı 30 dB tavanının ALTINA indirmek için güçlü bir eş-kanal
// girişimci eklenir; aksi halde girişimsiz ortamda her iki istasyon da
// tavana yapışır ve fark görünmez.
func TestPicoLowerPowerThanMacro(t *testing.T) {
	makeScenario := func(pico bool) float64 {
		bs := NewBaseStation(0, 0, 0)
		bs.UserX, bs.UserY = 80, 0
		bs.ServingLOS = true
		bs.ServingShadowDB = 0
		if pico {
			bs.IsPico, bs.TxWatts, bs.HeightM = true, PicoTxWatts, PicoHeightM
		}
		// Kullanıcının hemen yanında güçlü bir makro girişimci: SINR'ı
		// tavanın altına indirir ki servis gücü farkı SINR'a yansısın.
		interferer := NewBaseStation(1, 90, 0) // UE_A'ya 10 m
		bs.Interferers = []Agent_ID{1}
		bs.InterfLOS[1] = true
		bs.InterfShadowDB[1] = 0

		net := []*BaseStation{bs, interferer}
		return SINRForColor(bs, 0, indexByID(net), map[Agent_ID]PRB{0: 0, 1: 0})
	}
	sinrMacro := makeScenario(false)
	sinrPico := makeScenario(true)

	if !(sinrPico < sinrMacro) {
		t.Fatalf("piko SINR (%.2f dB) makrodan (%.2f dB) düşük olmalıydı",
			10*math.Log10(sinrPico), 10*math.Log10(sinrMacro))
	}
	// Servis gücü 40/1 = 16 dB farklı; girişim iki senaryoda da aynı
	// (girişimci hep makro), yani SINR farkı servis gücü farkını yansıtır.
	diffDB := 10*math.Log10(sinrMacro) - 10*math.Log10(sinrPico)
	if diffDB < 10 {
		t.Fatalf("makro-piko SINR farkı beklenenden küçük: %.1f dB", diffDB)
	}
}

// TestPicoInterferesLess: bir piko girişimci, aynı konumdaki makro
// girişimciden daha az girişim üretmeli (düşük güç).
func TestPicoInterferesLess(t *testing.T) {
	build := func(interfPico bool) float64 {
		victim := NewBaseStation(0, 0, 0)
		victim.UserX, victim.UserY = 50, 0
		victim.ServingLOS = true

		interferer := NewBaseStation(1, 80, 0)
		if interfPico {
			interferer.IsPico, interferer.TxWatts, interferer.HeightM = true, PicoTxWatts, PicoHeightM
		}
		victim.Interferers = []Agent_ID{1}
		victim.InterfLOS[1] = true
		victim.InterfShadowDB[1] = 0

		net := []*BaseStation{victim, interferer}
		return interferencePowerAt(victim, 0, indexByID(net), map[Agent_ID]PRB{0: 0, 1: 0})
	}
	macroInterf := build(false)
	picoInterf := build(true)

	if !(picoInterf < macroInterf) {
		t.Fatalf("piko girişimci (%.3e W) makrodan (%.3e W) az girişim yapmalıydı", picoInterf, macroInterf)
	}
}

// TestHetNetReproducible: aynı seed aynı istasyon tiplerini üretmeli.
func TestHetNetReproducible(t *testing.T) {
	types := func() []bool {
		rng := rand.New(rand.NewSource(7))
		net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)
		out := make([]bool, len(net))
		for i, bs := range net {
			out[i] = bs.IsPico
		}
		return out
	}
	a, b := types(), types()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("BS-%d tipi tekrarlanmadı", i)
		}
	}
}

// TestCouplingSymmetricWithHetNet: A3 sonrası kuplaj ağırlığı hâlâ
// simetrik olmalı — w_ij = P_j·G(j→UE_i) + P_i·G(i→UE_j), i↔j takasında
// terimler yer değiştirir. Simetri korunmazsa potential game bozulur.
func TestCouplingSymmetricWithHetNet(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)
	byID := indexByID(net)
	for _, bs := range net {
		for nid, w := range bs.NeighborWeights {
			back := byID[nid].NeighborWeights[bs.ID]
			if math.Abs(w-back) > math.Abs(w)*1e-12 {
				t.Fatalf("HetNet'te simetri bozuldu: w(%d,%d)=%.6e != %.6e", bs.ID, nid, w, back)
			}
		}
	}
}
