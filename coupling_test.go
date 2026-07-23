package main

import (
	"math"
	"math/rand"
	"testing"
)

// ============================================================
// A1 DOĞRULAMA: FİZİKSEL KUPLAJ AĞIRLIĞI
//
// w_ij = ½ · Ptx · [ G(j -> UE_i) + G(i -> UE_j) ]
//
// Bu testler üç şeyi garanti eder:
//  1. SİMETRİ — exact potential game yapısının (ve H-2'deki yakınsama
//     argümanının) ön koşulu.
//  2. FİZİKSEL TUTARLILIK — ağırlık, gerçekten iki yönlü girişim
//     gücünün ortalamasıdır; PHY katmanının ölçtüğü nicelikle aynı
//     modelden gelir.
//  3. TEKRARLANABİLİRLİK — aynı seed aynı ağırlıkları üretir.
// ============================================================

func withCoupling(mode CouplingKind, fn func()) {
	orig := CouplingMode
	CouplingMode = mode
	defer func() { CouplingMode = orig }()
	fn()
}

// TestCouplingWeightsSymmetric: w_ij == w_ji, HER İKİ modda da.
// Simetri bozulursa oyun artık exact potential game olmaz ve
// H-2'nin yakınsama gerekçesi çöker.
func TestCouplingWeightsSymmetric(t *testing.T) {
	for _, mode := range []CouplingKind{CouplingPhysical, CouplingGeometric} {
		withCoupling(mode, func() {
			rng := rand.New(rand.NewSource(11))
			net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)
			byID := indexByID(net)

			edges := 0
			for _, bs := range net {
				for nid, w := range bs.NeighborWeights {
					back, ok := byID[nid].NeighborWeights[bs.ID]
					if !ok {
						t.Fatalf("[%v] BS-%d -> BS-%d kenarı tek yönlü", mode, bs.ID, nid)
					}
					if w != back {
						t.Fatalf("[%v] simetri ihlali: w(%d,%d)=%.6e != w(%d,%d)=%.6e",
							mode, bs.ID, nid, w, nid, bs.ID, back)
					}
					if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
						t.Fatalf("[%v] geçersiz ağırlık w(%d,%d)=%v", mode, bs.ID, nid, w)
					}
					edges++
				}
			}
			if edges == 0 {
				t.Fatalf("[%v] hiç kenar üretilmedi", mode)
			}
		})
	}
}

// TestPhysicalCouplingMatchesInterference: elle kurulmuş iki
// istasyonda ağırlık, kapalı formdaki ortalama girişim gücüne eşit
// olmalı — "ağırlık artık fiziksel bir nicelik" iddiasının testi.
func TestPhysicalCouplingMatchesInterference(t *testing.T) {
	withCoupling(CouplingPhysical, func() {
		// A orijinde, kullanıcısı (50,0)'da. B (80,0)'da, kullanıcısı (140,0)'da.
		// B -> UE_A mesafesi 30 m; A -> UE_B mesafesi 140 m.
		a := NewBaseStation(0, 0, 0)
		b := NewBaseStation(1, 80, 0)
		a.UserX, a.UserY = 50, 0
		b.UserX, b.UserY = 140, 0
		a.Neighbros = []Agent_ID{1}
		b.Neighbros = []Agent_ID{0}
		a.InterfLOS[1], a.InterfShadowDB[1] = true, 0
		b.InterfLOS[0], b.InterfShadowDB[0] = true, 0

		net := []*BaseStation{a, b}
		assignCouplingWeights(net, rand.New(rand.NewSource(1)), false)

		gBtoUEA := math.Pow(10, -PathLossUMa(30, true)/10)
		gAtoUEB := math.Pow(10, -PathLossUMa(140, true)/10)
		want := TxPowerWatts * (gBtoUEA + gAtoUEB)

		if got := a.NeighborWeights[1]; math.Abs(got-want)/want > 1e-12 {
			t.Fatalf("fiziksel ağırlık: got %.9e, want %.9e", got, want)
		}
		// Simetri elle de doğrulansın
		if a.NeighborWeights[1] != b.NeighborWeights[0] {
			t.Fatal("fiziksel ağırlık simetrik değil")
		}
	})
}

// TestPhysicalCouplingSeesNearbyInterferer: A1'in varlık sebebi.
// İki BS birbirinden UZAK (geometrik vekil zayıf kenar der) ama
// girişimci, karşı tarafın kullanıcısının DİBİNDE. Fiziksel ağırlık
// bu kenarı AĞIR, geometrik vekil ise HAFİF görmelidir.
func TestPhysicalCouplingSeesNearbyInterferer(t *testing.T) {
	build := func(mode CouplingKind) float64 {
		var w float64
		withCoupling(mode, func() {
			// A (0,0), kullanıcısı (98,0) — neredeyse B'nin dibinde.
			// B (100,0): BS-BS mesafesi 100 m (zayıf kenar),
			// ama B -> UE_A mesafesi yalnızca 2 m.
			a := NewBaseStation(0, 0, 0)
			b := NewBaseStation(1, 100, 0)
			a.UserX, a.UserY = 98, 0
			b.UserX, b.UserY = 160, 0 // B'nin kullanıcısı A'dan uzakta
			a.Neighbros = []Agent_ID{1}
			b.Neighbros = []Agent_ID{0}
			a.InterfLOS[1], a.InterfShadowDB[1] = true, 0
			b.InterfLOS[0], b.InterfShadowDB[0] = true, 0

			net := []*BaseStation{a, b}
			assignCouplingWeights(net, rand.New(rand.NewSource(1)), false)
			w = a.NeighborWeights[1]
		})
		return w
	}

	// Referans: BS'lerin de kullanıcıların da uzak olduğu "gerçekten
	// zayıf" bir kenar. Aynı BS-BS mesafesi (100 m), ama girişimci
	// hiçbir kullanıcının yakınında değil.
	var farW float64
	withCoupling(CouplingPhysical, func() {
		a := NewBaseStation(0, 0, 0)
		b := NewBaseStation(1, 100, 0)
		a.UserX, a.UserY = 0, -60 // kendi BS'ine yakın, B'den uzak
		b.UserX, b.UserY = 160, 0
		a.Neighbros = []Agent_ID{1}
		b.Neighbros = []Agent_ID{0}
		a.InterfLOS[1], a.InterfShadowDB[1] = true, 0
		b.InterfLOS[0], b.InterfShadowDB[0] = true, 0
		net := []*BaseStation{a, b}
		assignCouplingWeights(net, rand.New(rand.NewSource(1)), false)
		farW = a.NeighborWeights[1]
	})

	nearW := build(CouplingPhysical)
	if !(nearW > 10*farW) {
		t.Fatalf("fiziksel kuplaj yakın girişimciyi ayırt edemedi: yakın %.3e vs uzak %.3e", nearW, farW)
	}

	// Geometrik vekil için iki durum AYNI BS-BS mesafesine sahip
	// olduğundan ayırt edilemez olmalı (A1'in çözdüğü körlük).
	withCoupling(CouplingGeometric, func() {
		a := NewBaseStation(0, 0, 0)
		b := NewBaseStation(1, 100, 0)
		a.Neighbros = []Agent_ID{1}
		b.Neighbros = []Agent_ID{0}
		net := []*BaseStation{a, b}
		assignCouplingWeights(net, rand.New(rand.NewSource(1)), false)
		geoW := a.NeighborWeights[1]
		// Geometrik ağırlık yalnızca mesafeye bağlıdır; kullanıcı
		// konumlarından bağımsız olduğu için yukarıdaki iki senaryoyu
		// da aynı değerle fiyatlar. Burada sadece hesaplanabildiğini
		// ve pozitif olduğunu doğruluyoruz.
		if geoW <= 0 {
			t.Fatal("geometrik ağırlık hesaplanamadı")
		}
	})
}

// TestCouplingReproducible: aynı seed -> aynı ağırlıklar (her iki mod).
func TestCouplingReproducible(t *testing.T) {
	for _, mode := range []CouplingKind{CouplingPhysical, CouplingGeometric} {
		withCoupling(mode, func() {
			snap := func() []float64 {
				rng := rand.New(rand.NewSource(77))
				net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)
				out := make([]float64, 0, 256)
				for _, bs := range net {
					for _, nid := range bs.Neighbros {
						out = append(out, bs.NeighborWeights[nid])
					}
				}
				return out
			}
			a, b := snap(), snap()
			if len(a) != len(b) {
				t.Fatalf("[%v] kenar sayısı tekrarlanmadı: %d vs %d", mode, len(a), len(b))
			}
			for i := range a {
				if a[i] != b[i] {
					t.Fatalf("[%v] ağırlık %d tekrarlanmadı: %.9e != %.9e", mode, i, a[i], b[i])
				}
			}
		})
	}
}

// TestPotentialGameDecrease: exact potential game özelliğinin doğrudan
// testi. Simetrik ağırlıklarda, tek bir oyuncunun maliyetini azaltan
// her tek taraflı sapma, GLOBAL potansiyeli (toplam çakışma maliyetini)
// tam olarak aynı miktarda azaltmalıdır. H-2'nin yakınsama gerekçesi
// birebir bu özelliğe dayanır.
func TestPotentialGameDecrease(t *testing.T) {
	rng := rand.New(rand.NewSource(2024))
	net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)
	colors := RandomAssignment(net, rng) // rastgele (dengede olmayan) durum

	for _, bs := range net {
		cur := colors[bs.ID]
		curCost := 0.0
		for nid, w := range bs.NeighborWeights {
			if colors[nid] == cur {
				curCost += w
			}
		}
		for c := PRB(0); c < MaxColors; c++ {
			if c == cur {
				continue
			}
			newCost := 0.0
			for nid, w := range bs.NeighborWeights {
				if colors[nid] == c {
					newCost += w
				}
			}
			if newCost >= curCost {
				continue // iyileştirme değil
			}

			before := AssignmentCost(net, colors)
			colors[bs.ID] = c
			after := AssignmentCost(net, colors)
			colors[bs.ID] = cur // geri al

			deltaPlayer := newCost - curCost // oyuncunun kazancı (negatif)
			deltaGlobal := after - before    // potansiyelin değişimi
			if math.Abs(deltaPlayer-deltaGlobal) > math.Abs(deltaPlayer)*1e-9 {
				t.Fatalf("potential game ihlali: oyuncu Δ=%.6e, global Δ=%.6e", deltaPlayer, deltaGlobal)
			}
		}
	}
}

// TestCouplingModesShareTopologyAndChannel: -coupling ablasyonunun
// EŞLEŞTİRİLMİŞ bir deney olabilmesi için, iki mod aynı seed'de birebir
// aynı topolojiyi ve aynı donmuş kanalı üretmelidir; yalnızca kenar
// ağırlıkları farklı olmalıdır.
func TestCouplingModesShareTopologyAndChannel(t *testing.T) {
	snap := func(mode CouplingKind) (pos, chn []float64, deg []int) {
		withCoupling(mode, func() {
			rng := rand.New(rand.NewSource(555))
			net := BuildNetwork(rng, 35, SimAreaSize, SimThreshold, false)
			for _, bs := range net {
				pos = append(pos, bs.X, bs.Y, bs.UserX, bs.UserY)
				chn = append(chn, bs.ServingShadowDB)
				if bs.ServingLOS {
					chn = append(chn, 1)
				} else {
					chn = append(chn, 0)
				}
				deg = append(deg, len(bs.Neighbros))
			}
		})
		return
	}

	pA, cA, dA := snap(CouplingPhysical)
	pB, cB, dB := snap(CouplingGeometric)

	if len(pA) != len(pB) || len(cA) != len(cB) || len(dA) != len(dB) {
		t.Fatal("iki mod farklı boyutta ağ üretti")
	}
	for i := range pA {
		if pA[i] != pB[i] {
			t.Fatalf("konum/UE %d modlar arasında farklı: %.6f != %.6f", i, pA[i], pB[i])
		}
	}
	for i := range cA {
		if cA[i] != cB[i] {
			t.Fatalf("kanal durumu %d modlar arasında farklı", i)
		}
	}
	for i := range dA {
		if dA[i] != dB[i] {
			t.Fatalf("BS-%d komşu sayısı farklı: %d != %d", i, dA[i], dB[i])
		}
	}
}

// TestPotentialEqualsTotalInterference: A1'in en güçlü iddiası —
// oyunun potansiyel fonksiyonu (AssignmentCost) TAM OLARAK ağın
// toplam eş-kanal girişim gücüne eşittir. Yani ajanların minimize
// ettiği soyut nicelik ile PHY katmanının ölçtüğü fiziksel nicelik
// aynı şeydir.
func TestPotentialEqualsTotalInterference(t *testing.T) {
	withCoupling(CouplingPhysical, func() {
		rng := rand.New(rand.NewSource(31337))
		net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)
		colors := RandomAssignment(net, rng)
		byID := indexByID(net)

		// Doğrudan ölçüm: her kullanıcının konumunda toplanan girişim.
		measured := 0.0
		for _, bs := range net {
			measured += interferencePowerAt(bs, colors[bs.ID], byID, colors)
		}

		potential := AssignmentCost(net, colors)
		if math.Abs(measured-potential) > measured*1e-9 {
			t.Fatalf("potansiyel != toplam girişim: %.9e vs %.9e", potential, measured)
		}
	})
}
