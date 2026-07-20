package main

import "math"

// ------------- Yardımcı olacak fonksiyonlarr -------------

func Distance(a, b *BaseStation) float64 { //MESAFE HESABI
	return math.Sqrt(math.Pow(a.X-b.X, 2) + math.Pow(a.Y-b.Y, 2))
}

//  Jain's Fairness Index Fonksiyonu
// Formül: (Toplam x)^2 / (n * Toplam x^2)

func CalculateJainsFairness(network []*BaseStation) float64 {
	var sumThroughput float64
	var sumSquareThroughput float64
	n := float64(len(network))

	for _, bs := range network {
		xi := bs.Throughput
		sumThroughput += xi
		sumSquareThroughput += (xi * xi)
	}

	if sumSquareThroughput == 0 {
		return 0
	}

	jainsIndex := (sumThroughput * sumThroughput) / (n * sumSquareThroughput)
	return jainsIndex
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

// ============================================================
// H-2 DENETİMİ: NASH DENGESİ DOĞRULAMASI
//
// Simülasyon bittikten SONRA, her istasyonun GERÇEK nihai komşu
// renklerine göre (ajanın kendi NeighborMap'i değil, ağın yer
// gerçeği) en-iyi-yanıtını yeniden hesaplar. Mevcut renginden
// kesin olarak daha düşük maliyetli bir renge geçmek isteyen
// istasyon sayısını döndürür.
//
// Tanım gereği bu sayı 0 değilse nihai tahsis Nash dengesi
// DEĞİLDİR; "NASH EQUILIBRIUM REACHED" iddiası ancak bu denetim
// 0 ihlal verdiğinde yapılabilir.
// ============================================================

// interferenceOfColor: bs 'c' rengini seçseydi, verilen gerçek renk
// haritasına göre katlanacağı toplam girişim ağırlığı.
func interferenceOfColor(bs *BaseStation, c PRB, trueColors map[Agent_ID]PRB) float64 {
	total := 0.0
	for neighborID, weight := range bs.NeighborWeights {
		if nc, ok := trueColors[neighborID]; ok && nc != -1 && nc == c {
			total += weight
		}
	}
	return total
}

// VerifyNashEquilibrium: tek taraflı sapma isteği olan istasyonların
// sayısını döndürür (uncommitted istasyonlar ayrı sayılır).
func VerifyNashEquilibrium(network []*BaseStation) (violations, uncommitted int) {
	trueColors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		trueColors[bs.ID] = bs.CurrentPRB
	}

	for _, bs := range network {
		if bs.CurrentPRB == -1 {
			uncommitted++
			continue
		}
		currentCost := interferenceOfColor(bs, bs.CurrentPRB, trueColors)
		for c := PRB(0); c < MaxColors; c++ {
			if c == bs.CurrentPRB {
				continue
			}
			// Kesin iyileşme var mı? (küçük görece pay: float gürültüsüne karşı)
			if interferenceOfColor(bs, c, trueColors) < currentCost*(1-1e-9) {
				violations++
				break
			}
		}
	}
	return violations, uncommitted
}

// MERKEZİ GREEDY REFERANS (BASELINE) SARMALAYICISI
//
// DİKKAT: Bu, gerçek optimum DEĞİL, sezgisel merkezi greedy çözümüdür;
// buna oranlanan metrik "Price of Anarchy" DEĞİL, "Gain over Greedy"dir.
// Gerçek optimum için optimum.go / BruteForceOptimum'a bakınız.
// Tahsis mantığı baselines.go'ya taşındı (Y-1); maliyet tanımı tüm
// şemalarla ortaktır (AssignmentCost).
func CalculateGreedyBaseline(network []*BaseStation) float64 {
	return AssignmentCost(network, GreedyAssignment(network))
}
