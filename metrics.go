package main

import "math"

// ------------- Yardımcı metrik fonksiyonları -------------

func Distance(a, b *BaseStation) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

// JainOf: Jain's Fairness Index — (Σx)² / (n·Σx²).
// Baseline karşılaştırmasında şemaların hız vektörleri doğrudan buna verilir.
func JainOf(xs []float64) float64 {
	var sum, sumSq float64
	for _, x := range xs {
		sum += x
		sumSq += x * x
	}
	if sumSq == 0 {
		return 0
	}
	return (sum * sum) / (float64(len(xs)) * sumSq)
}

func CalculateJainsFairness(network []*BaseStation) float64 {
	xs := make([]float64, len(network))
	for i, bs := range network {
		xs[i] = bs.Throughput
	}
	return JainOf(xs)
}

// GLOBAL AMAÇ FONKSİYONU (toplam ağ çakışma maliyeti).
// Artık ortak AssignmentCost tanımının ince bir sarmalayıcısıdır:
// dağıtık NE ile bütün baseline'lar AYNI maliyet tanımını paylaşır.
func CalculateGlobalObjective(network []*BaseStation) float64 {
	assign := make([]PRB, len(network))
	for i, bs := range network {
		assign[i] = bs.CurrentPRB
	}
	return AssignmentCost(network, assign)
}

// MERKEZİ GREEDY REFERANS (BASELINE) HESAPLAYICISI
//
// DİKKAT: Bu fonksiyon gerçek optimumu DEĞİL, sezgisel (heuristic) bir
// merkezi greedy çözümü hesaplar. Bu yüzden buna oranlanan metrik
// "Price of Anarchy" DEĞİLDİR; doğru adı "Gain over Greedy"dir.
// Gerçek optimum için optimum.go içindeki BruteForceOptimum'a bakınız.
//
// Uygulama, baselines.go'daki ortak altyapıyı kullanır: atama
// GreedyAssignment ile üretilir, maliyet tüm şemalarla AYNI tanımı
// kullanan AssignmentCost ile hesaplanır (tanım farkı riski yok).
func CalculateGreedyBaseline(network []*BaseStation) float64 {
	return AssignmentCost(network, GreedyAssignment(network))
}
