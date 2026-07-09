package main

import (
	"math"
	"math/rand"
)

// ============================================================
// MERKEZİ REFERANS ŞEMALARI (BASELINES)
//
// Dağıtık NE'nin "iyi" olup olmadığı ancak referanslarla anlaşılır:
//   - Greedy      : zorluk-sıralı merkezi sezgisel (akıllı üst çıta)
//   - DSATUR      : klasik doygunluk-dereceli renklendirmenin
//                   ağırlıklı-K uyarlaması (ikinci akıllı çıta)
//   - Sabit reuse : coğrafyayı bilmeyen statik plan, i mod K (naif çıta)
//   - Rastgele    : alt sınır referansı
//
// KRİTİK TASARIM KURALI: Bütün şemalar maliyeti TEK ortak fonksiyonla
// (AssignmentCost) hesaplar. Şemalar arasında maliyet tanımı farkı
// olması bu yapıda imkânsızdır.
// ============================================================

// AssignmentCost: verilen atamada aynı-renk komşu çiftlerinin kenar
// ağırlıkları toplamı (her kenar bir kez). Renksiz (-1) düğüm çakışma
// üretmez. Tanım, CalculateGlobalObjective ve BruteForceOptimum ile aynıdır.
func AssignmentCost(network []*BaseStation, assign []PRB) float64 {
	idx := indexOf(network)
	cost := 0.0
	for i, bs := range network {
		if assign[i] == -1 {
			continue
		}
		for nid, w := range bs.NeighborWeights {
			j := idx[nid]
			if j > i && assign[j] == assign[i] { // her kenar bir kez
				cost += w
			}
		}
	}
	return cost
}

func totalNeighborWeight(bs *BaseStation) float64 {
	w := 0.0
	for _, x := range bs.NeighborWeights {
		w += x
	}
	return w
}

// GreedyAssignment: istasyonları "zorluk"a göre (toplam komşu ağırlığı,
// büyükten küçüğe) sıralar; her birine, o ana dek renklenmiş komşularına
// göre en az ağırlıklı cezayı getiren rengi verir.
func GreedyAssignment(network []*BaseStation) []PRB {
	idx := indexOf(network)
	n := len(network)

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	// zorluk = toplam komşu ağırlığı; büyükten küçüğe
	for i := 0; i < n; i++ {
		for j := 0; j < n-i-1; j++ {
			if totalNeighborWeight(network[order[j]]) < totalNeighborWeight(network[order[j+1]]) {
				order[j], order[j+1] = order[j+1], order[j]
			}
		}
	}

	assign := make([]PRB, n)
	for i := range assign {
		assign[i] = -1
	}
	for _, i := range order {
		bs := network[i]
		bestColor := PRB(0)
		minImpact := math.MaxFloat64
		for c := PRB(0); c < PRB(MaxColors); c++ {
			impact := 0.0
			for nid, w := range bs.NeighborWeights {
				if ac := assign[idx[nid]]; ac != -1 && ac == c {
					impact += w
				}
			}
			if impact < minImpact {
				minImpact = impact
				bestColor = c
			}
		}
		assign[i] = bestColor
	}
	return assign
}

// DSATURAssignment: klasik DSATUR'un ağırlıklı-K uyarlaması.
// Her adımda doygunluğu (renklenmiş komşulardaki FARKLI renk sayısı) en
// yüksek düğüm seçilir (eşitlikte toplam komşu ağırlığı büyük olan);
// düğüme, renklenmiş komşularına göre en az ağırlıklı ceza getiren renk verilir.
// K >= kromatik sayı ise klasik DSATUR gibi sıfır-çakışma üretme eğilimindedir.
func DSATURAssignment(network []*BaseStation) []PRB {
	idx := indexOf(network)
	n := len(network)
	assign := make([]PRB, n)
	for i := range assign {
		assign[i] = -1
	}

	for step := 0; step < n; step++ {
		// En yüksek doygunluklu renksiz düğümü bul
		best, bestSat, bestW := -1, -1, -1.0
		for i, bs := range network {
			if assign[i] != -1 {
				continue
			}
			seen := map[PRB]bool{}
			for nid := range bs.NeighborWeights {
				if c := assign[idx[nid]]; c != -1 {
					seen[c] = true
				}
			}
			sat := len(seen)
			w := totalNeighborWeight(bs)
			if sat > bestSat || (sat == bestSat && w > bestW) {
				best, bestSat, bestW = i, sat, w
			}
		}

		// Ona en az cezalı rengi ver
		bs := network[best]
		bestColor := PRB(0)
		minImpact := math.MaxFloat64
		for c := PRB(0); c < PRB(MaxColors); c++ {
			impact := 0.0
			for nid, w := range bs.NeighborWeights {
				if ac := assign[idx[nid]]; ac != -1 && ac == c {
					impact += w
				}
			}
			if impact < minImpact {
				minImpact = impact
				bestColor = c
			}
		}
		assign[best] = bestColor
	}
	return assign
}

// FixedReuseAssignment: planlama yapmayan sabit yeniden kullanım —
// renkler ID sırasına göre döngüsel dağıtılır (i mod K). Coğrafyayı
// bilmeyen "statik plan" referansıdır.
func FixedReuseAssignment(network []*BaseStation, k int) []PRB {
	assign := make([]PRB, len(network))
	for i := range network {
		assign[i] = PRB(i % k)
	}
	return assign
}

// RandomAssignment: tamamen rastgele tahsis — alt sınır referansı.
func RandomAssignment(network []*BaseStation, k int, rng *rand.Rand) []PRB {
	assign := make([]PRB, len(network))
	for i := range network {
		assign[i] = PRB(rng.Intn(k))
	}
	return assign
}
