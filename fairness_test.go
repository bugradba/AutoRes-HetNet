package main

import (
	"math"
	"math/rand"
	"testing"
)

// ============================================================
// A4 DOĞRULAMA: ADALET (WEIGHTED POTENTIAL GAME)
//
// Adalet, kenar ağırlığına simetrik bir çarpan ekleyerek uygulanır:
//   w'_ij = ½(alpha_i + alpha_j) · w_ij
// alpha_i, istasyonun girişimSİZ servis SINR'ından türeyen, TAHSİSTEN
// BAĞIMSIZ bir özelliktir. Bu iki koşul (simetri + tahsis-bağımsızlık)
// birlikte ağırlıklı potansiyel oyun yapısını ve dolayısıyla H-2'deki
// yakınsama garantisini korur.
//
// Bu testler A4'ün bilimsel geçerliliğinin dayanağıdır: eğer bu
// özellikler bozulsaydı, "adalet ekledik ama yakınsama garantisi hâlâ
// geçerli" iddiası çökerdi.
// ============================================================

func withFairness(beta float64, fn func()) {
	orig := FairnessBeta
	FairnessBeta = beta
	defer func() { FairnessBeta = orig }()
	fn()
}

// TestFairnessWeightsStaySymmetric: adalet açıkken bile w'_ij = w'_ji.
// Simetri, exact/weighted potential game'in ön koşuludur.
func TestFairnessWeightsStaySymmetric(t *testing.T) {
	withFairness(3.0, func() {
		rng := rand.New(rand.NewSource(11))
		net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)
		byID := indexByID(net)
		for _, bs := range net {
			for nid, w := range bs.NeighborWeights {
				back := byID[nid].NeighborWeights[bs.ID]
				if math.Abs(w-back) > math.Abs(w)*1e-12 {
					t.Fatalf("adalet simetriyi bozdu: w(%d,%d)=%.6e != %.6e", bs.ID, nid, w, back)
				}
			}
		}
	})
}

// TestFairnessAlphaIndependentOfAllocation: alpha, istasyonun renginden
// ve komşularının renklerinden BAĞIMSIZ olmalı — yalnızca kendi
// girişimsiz servis SINR'ına bağlı. Bu, ağırlıklı potansiyel yapının
// korunmasının anahtarıdır (alpha tahsise bağlı olsaydı potansiyel
// fonksiyonu iyi tanımlı olmazdı).
func TestFairnessAlphaIndependentOfAllocation(t *testing.T) {
	withFairness(3.0, func() {
		rng := rand.New(rand.NewSource(7))
		net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)

		// alpha'ları kaydet
		alpha0 := make([]float64, len(net))
		for i, bs := range net {
			alpha0[i] = bs.FairnessAlpha
		}

		// Farklı tahsisler dene — alpha değişmemeli
		for _, bs := range net {
			bs.CurrentPRB = PRB(rng.Intn(int(MaxColors)))
		}
		for i, bs := range net {
			if bs.FairnessAlpha != alpha0[i] {
				t.Fatalf("BS-%d alpha tahsise göre değişti: %.6f -> %.6f",
					bs.ID, alpha0[i], bs.FairnessAlpha)
			}
		}
	})
}

// TestFairnessHighInterferenceGetsHigherAlpha: EN KÖTÜ DURUM SINR'ı
// (tüm komşular eş-kanal) referansın altındaki istasyon alpha>1
// almalı. Teşhis, hücre kenarı istasyonlarının zayıf-sinyalli değil
// yüksek-girişim-maruziyetli olduğunu gösterdi; alpha bu maruziyeti
// yakalar.
func TestFairnessHighInterferenceGetsHigherAlpha(t *testing.T) {
	withFairness(3.0, func() {
		rng := rand.New(rand.NewSource(42))
		net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)
		for _, bs := range net {
			// en kötü durum SINR'ı yeniden hesapla
			potInterf := 0.0
			for _, nid := range bs.Interferers {
				other := net[int(nid)]
				d := dist2D(other.X, other.Y, bs.UserX, bs.UserY)
				potInterf += other.TxWatts * LinkGain(d, bs.InterfLOS[nid], bs.InterfShadowDB[nid], other.HeightM)
			}
			dServ := dist2D(bs.X, bs.Y, bs.UserX, bs.UserY)
			sig := bs.TxWatts * LinkGain(dServ, bs.ServingLOS, bs.ServingShadowDB, bs.HeightM)
			worstSINRdB := 10 * math.Log10(sig/(potInterf+NoisePowerWatts()))

			if worstSINRdB < FairnessRefSINRdB && !(bs.FairnessAlpha > 1.0) {
				t.Fatalf("kırılgan BS-%d (en kötü SINR %.1f dB) alpha<=1 aldı: %.3f",
					bs.ID, worstSINRdB, bs.FairnessAlpha)
			}
			if worstSINRdB > FairnessRefSINRdB && !(bs.FairnessAlpha < 1.0) {
				t.Fatalf("sağlam BS-%d (en kötü SINR %.1f dB) alpha>=1 aldı: %.3f",
					bs.ID, worstSINRdB, bs.FairnessAlpha)
			}
		}
	})
}

// TestFairnessDisabledByDefault: FairnessBeta=0 iken tüm alpha=1 ve
// ağırlıklar A4 öncesiyle BİREBİR aynı olmalı (davranış-koruma).
func TestFairnessDisabledByDefault(t *testing.T) {
	// beta=0 (varsayılan) ile ağırlıkları al
	rng1 := rand.New(rand.NewSource(99))
	netOff := BuildNetwork(rng1, 30, SimAreaSize, SimThreshold, false)
	for _, bs := range netOff {
		if bs.FairnessAlpha != 1.0 {
			t.Fatalf("beta=0 iken BS-%d alpha=%.3f (1.0 olmalı)", bs.ID, bs.FairnessAlpha)
		}
	}
}

// TestFairnessPreservesPotentialGame: A4'ün EN KRİTİK testi. Ağırlıklı
// potansiyel oyunda, bir oyuncunun tek taraflı sapmasının kendi
// maliyetindeki değişim, global potansiyeldeki (AssignmentCost)
// değişime TAM eşit olmalı. Bu özellik, yakınsama garantisinin
// (H-2) adalet açıkken de geçerli olduğunun kanıtıdır.
func TestFairnessPreservesPotentialGame(t *testing.T) {
	withFairness(3.0, func() {
		rng := rand.New(rand.NewSource(2024))
		net := BuildNetwork(rng, 30, SimAreaSize, SimThreshold, false)
		colors := RandomAssignment(net, rng)

		for _, bs := range net {
			cur := colors[bs.ID]
			// oyuncunun mevcut maliyeti (kendi ağırlıklı çakışmaları)
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
				before := AssignmentCost(net, colors)
				colors[bs.ID] = c
				after := AssignmentCost(net, colors)
				colors[bs.ID] = cur // geri al

				deltaPlayer := newCost - curCost
				deltaGlobal := after - before
				// Göreli tolerans: AssignmentCost tüm ağı toplayıp 2'ye
				// böldüğü için mutlak büyüklük oyuncunun kendi toplamından
				// çok daha büyük olabilir; float yuvarlaması bu ölçekte
				// ~1e-9 göreli fark bırakır. Bu, potansiyel yapının
				// bozulması DEĞİL, normal aritmetik gürültüdür.
				scale := math.Max(math.Abs(deltaPlayer), math.Abs(before))
				if math.Abs(deltaPlayer-deltaGlobal) > scale*1e-9 {
					t.Fatalf("ağırlıklı potansiyel oyun ihlali: oyuncu Δ=%.9e, global Δ=%.9e",
						deltaPlayer, deltaGlobal)
				}
			}
		}
	})
}

// TestCellEdgeIsCapacityBound: A4'ün ana bulgusu. N=40 yoğunlukta
// çakışma güvercin yuvası gereği kaçınılmazdır (çok istasyonun komşu
// sayısı >= K) ve hücre kenarı istasyonları zaten en az yüklü rengi
// seçmiştir — yani sorun tahsis değil kapasitedir. Bu test o yapısal
// gerçeği (komşu sayısı >= K olan istasyon oranı) sabitler.
func TestCellEdgeIsCapacityBound(t *testing.T) {
	overloaded, total := 0, 0
	for seed := int64(42); seed < 62; seed++ {
		rng := rand.New(rand.NewSource(seed))
		net := BuildNetwork(rng, 40, SimAreaSize, SimThreshold, false)
		for _, bs := range net {
			total++
			if len(bs.Neighbors) >= int(MaxColors) {
				overloaded++
			}
		}
	}
	frac := float64(overloaded) / float64(total)
	// N=40, K=5, 400x400 m'de istasyonların çoğu K'den fazla komşuya
	// sahip olmalı (çakışma kaçınılmaz). En az yarısı beklenir.
	if frac < 0.5 {
		t.Fatalf("beklenenden az yoğunluk: istasyonların yalnızca %.0f%%'i >= K komşuya sahip", 100*frac)
	}
	t.Logf("yoğunluk: istasyonların %.0f%%'i >= K=%d komşuya sahip (çakışma kaçınılmaz)",
		100*frac, MaxColors)
}
