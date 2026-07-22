package main

import (
	"math"
	"testing"
)

// ============================================================
// İSTATİSTİK MOTORU TESTLERİ
//
// Eşleştirilmiş karşılaştırma tablosunun tamamı tTestPValue'ya
// dayanıyor; hatalı bir p-değeri yayınlanmış tüm sonuçları
// geçersiz kılar. Aşağıdaki referans değerler standart
// t-dağılımı tablolarından/SciPy'den alınmıştır.
// ============================================================

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.6f, want %.6f (tol %.g)", what, got, want, tol)
	}
}

// TestTTestKnownValues: t-dağılımının iki yönlü p-değerleri.
// Referanslar: t=2.228, df=10 -> p=0.050 (klasik %5 kritik değeri);
// t=1.96, df=1e6 -> p~0.05 (normale yakınsama); t=0 -> p=1.
func TestTTestKnownValues(t *testing.T) {
	approx(t, tTestPValue(2.228, 10), 0.050, 1e-3, "t=2.228, df=10")
	approx(t, tTestPValue(3.169, 10), 0.010, 1e-3, "t=3.169, df=10")
	approx(t, tTestPValue(2.0, 60), 0.0500, 1e-3, "t=2.0, df=60")
	approx(t, tTestPValue(1.96, 1000000), 0.0500, 1e-3, "t=1.96, df=1e6")
	approx(t, tTestPValue(0, 10), 1.0, 1e-12, "t=0")

	// Monotonluk: |t| büyüdükçe p küçülmeli
	prev := 1.0
	for _, tv := range []float64{0.5, 1.0, 2.0, 4.0, 8.0} {
		p := tTestPValue(tv, 25)
		if p >= prev {
			t.Fatalf("p monoton azalmalı: t=%.1f -> p=%.5f (önceki %.5f)", tv, p, prev)
		}
		prev = p
	}
}

// TestBetaIncBoundaries: I_x(a,b) sınır ve simetri özellikleri.
func TestBetaIncBoundaries(t *testing.T) {
	approx(t, betaInc(2, 3, 0), 0, 1e-12, "I_0")
	approx(t, betaInc(2, 3, 1), 1, 1e-12, "I_1")
	// Simetri: I_x(a,b) = 1 - I_{1-x}(b,a)
	for _, x := range []float64{0.1, 0.35, 0.5, 0.77, 0.9} {
		lhs := betaInc(2.5, 4.5, x)
		rhs := 1 - betaInc(4.5, 2.5, 1-x)
		approx(t, lhs, rhs, 1e-10, "simetri")
	}
	// a=b=1/2 -> arcsin dağılımı: I_x(0.5,0.5) = (2/π)·asin(√x)
	for _, x := range []float64{0.2, 0.5, 0.8} {
		approx(t, betaInc(0.5, 0.5, x), 2/math.Pi*math.Asin(math.Sqrt(x)), 1e-9, "arcsin")
	}
}

// TestPairedCompareKnownCase: elle hesaplanabilir küçük örnek.
// Farklar: -1, -2, -3, -4, -5 -> ortalama -3, s=1.5811, n=5
// SE = 0.70711, t = -4.2426, df=4 -> p ~ 0.01324
func TestPairedCompareKnownCase(t *testing.T) {
	a := []float64{10, 20, 30, 40, 50}
	b := []float64{11, 22, 33, 44, 55}
	r := PairedCompare(a, b)

	if r.N != 5 {
		t.Fatalf("N: got %d, want 5", r.N)
	}
	approx(t, r.MeanDiff, -3.0, 1e-9, "ortalama fark")
	approx(t, r.T, -4.2426, 1e-3, "t istatistiği")
	approx(t, r.P, 0.01324, 1e-4, "p-değeri")
	if r.ALower != 5 || r.Ties != 0 {
		t.Fatalf("kazanma sayımı: ALower=%d Ties=%d, want 5/0", r.ALower, r.Ties)
	}
	// Göreli fark: -3 / ortalama(b)=33 -> ~ -9.09%
	approx(t, r.RelPct, -9.0909, 1e-3, "göreli fark %")
}

// TestPairedIdenticalSeries: birebir aynı iki dizi -> fark 0, p=1.
func TestPairedIdenticalSeries(t *testing.T) {
	x := []float64{1.5, 2.5, 3.5, 4.5}
	r := PairedCompare(x, x)
	if r.MeanDiff != 0 || r.Ties != 4 || r.ALower != 0 {
		t.Fatalf("aynı diziler için sıfır fark beklenir: %+v", r)
	}
	approx(t, r.P, 1.0, 1e-12, "p")
}

// TestPairedBeatsUnpairedPower: eşleştirilmiş testin varlık sebebi.
// Ortak (koşudan gelen) büyük varyans + küçük ama tutarlı bir etki
// kurgulanır. Eşleştirilmiş analiz etkiyi yakalamalı; aynı veriye
// bakan bağımsız-örneklem yaklaşımı (GA'ların örtüşmesi) yakalayamamalı.
func TestPairedBeatsUnpairedPower(t *testing.T) {
	n := 40
	a := make([]float64, n)
	b := make([]float64, n)
	for i := 0; i < n; i++ {
		common := float64(i) * 10 // koşular arası devasa ortak varyans
		a[i] = common + 1.0
		b[i] = common + 1.2 // sabit, küçük ama gerçek etki
	}

	pr := PairedCompare(a, b)
	if !(pr.P < 0.001) {
		t.Fatalf("eşleştirilmiş test tutarlı etkiyi yakalamalıydı, p=%.4f", pr.P)
	}

	// Bağımsız-örneklem bakışı: ortalamaların GA'ları fazlasıyla örtüşür.
	if math.Abs(mean(a)-mean(b)) > ci95Half(a)+ci95Half(b) {
		t.Fatal("kurgu hatalı: bağımsız GA'lar örtüşmeliydi (eşleştirmenin kazancını gösteremiyoruz)")
	}
}
