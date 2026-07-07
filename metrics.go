package main

import "math"

// ------------- Yardımcı olacak fonksiyonlarr -------------

func Distance(a, b *BaseStation) float64 { //MESAFE HESABI
	return math.Sqrt(math.Pow(a.X-b.X, 2) + math.Pow(a.Y-b.Y, 2))
}

//  Jain's Fairness Index Fonksiyonu
// Formül: (Toplam x)^2 / (n * Toplam x^2)

// JainOf: herhangi bir hız vektörü için Jain indeksi. Baseline
// karşılaştırmasında şemaların hız vektörleri doğrudan buna verilir.
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

// GLOBAL AMAÇ FONKSİYONU (Total Network Interference)
// Tüm ağdaki toplam çatışma maliyetini hesaplar.
// Hedef: Bu değerin simülasyon sonunda azalmış olmasıdır.

func CalculateGlobalObjective(network []*BaseStation) float64 {
	totalCost := 0.0

	for _, bs := range network {
		if bs.CurrentPRB == -1 {
			continue
		}

		for neighborID, weight := range bs.NeighborWeights {
			var neighborColor PRB = -1
			for _, node := range network {
				if node.ID == neighborID {
					neighborColor = node.CurrentPRB
					break
				}
			}

			if neighborColor != -1 && bs.CurrentPRB == neighborColor {
				totalCost += weight
			}
		}
	}

	return totalCost / 2.0
}

// MERKEZİ GREEDY REFERANS (BASELINE) HESAPLAYICISI
//
// DİKKAT: Bu fonksiyon gerçek optimumu DEĞİL, sezgisel (heuristic) bir
// merkezi greedy çözümü hesaplar. Bu yüzden buna oranlanan metrik
// "Price of Anarchy" DEĞİLDİR; doğru adı "Gain over Greedy"dir.
// Gerçek optimum için optimum.go içindeki BruteForceOptimum'a bakınız.
//
// Uygulama artık baselines.go'daki ortak altyapıyı kullanır:
// atama GreedyAssignment ile üretilir, maliyet tüm şemalarla AYNI
// tanımı kullanan AssignmentCost ile hesaplanır (tanım farkı riski yok).
func CalculateGreedyBaseline(network []*BaseStation) float64 {
	return AssignmentCost(network, GreedyAssignment(network))
}
