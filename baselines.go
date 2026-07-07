package main

import (
	"math"
	"math/rand"
)

// ============================================================
// BASELINE TAHSİS YÖNTEMLERİ (yol haritası madde 5)
//
// "Greedy tek başına yeterli referans değil." Dağıtık çözümün
// konumlandırılması için dört merkezi referans:
//   - GreedyAssignment    : zorluk-sıralı açgözlü (eski baseline)
//   - DSATURAssignment    : klasik DSATUR'un ağırlıklı-K uyarlaması
//   - FixedReuseAssignment: planlamasız sabit frekans yeniden kullanımı
//   - RandomAssignment    : alt sınır referansı (rastgele tahsis)
//
// Hepsi atamayı []PRB (ağ dizisi sırasıyla) döndürür; maliyet tek bir
// ortak fonksiyonla (AssignmentCost) hesaplanır ki şemalar arasında
// tanım farkı oluşmasın.
// ============================================================

// indexOf: Agent_ID -> ağ dizisindeki konum.
func indexOf(network []*BaseStation) map[Agent_ID]int {
	idx := make(map[Agent_ID]int, len(network))
	for i, bs := range network {
		idx[bs.ID] = i
	}
	return idx
}

// AssignmentCost: verilen renk atamasının toplam çakışma maliyeti
// (aynı renkli komşu çiftlerinin kenar ağırlıkları; her kenar BİR kez).
// CalculateGlobalObjective ve BruteForceOptimum ile aynı tanımdır.
func AssignmentCost(network []*BaseStation, assign []PRB) float64 {
	idx := indexOf(network)
	total := 0.0
	for i, bs := range network {
		for nid, w := range bs.NeighborWeights {
			j := idx[nid]
			if j > i && assign[i] == assign[j] { // her kenar bir kez
				total += w
			}
		}
	}
	return total
}

// totalNeighborWeight: "zorluk derecesi" — toplam komşu ağırlığı.
func totalNeighborWeight(bs *BaseStation) float64 {
	s := 0.0
	for _, w := range bs.NeighborWeights {
		s += w
	}
	return s
}

// GreedyAssignment: eski CalculateGreedyBaseline'ın atama DÖNDÜREN hali.
// İstasyonlar zorluk derecesine göre (büyükten küçüğe) sıralanır; sırayla,
// o an en az ağırlıklı ceza getiren renk atanır. Optimum garantisi YOKTUR.
func GreedyAssignment(network []*BaseStation) []PRB {
	idx := indexOf(network)
	n := len(network)

	order := make([]*BaseStation, n)
	copy(order, network)
	// Basit bubble sort (n küçük; eski davranışla birebir aynı sıralama)
	for i := 0; i < n; i++ {
		for j := 0; j < n-i-1; j++ {
			if totalNeighborWeight(order[j]) < totalNeighborWeight(order[j+1]) {
				order[j], order[j+1] = order[j+1], order[j]
			}
		}
	}

	assign := make([]PRB, n)
	for i := range assign {
		assign[i] = -1
	}
	for _, bs := range order {
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
		assign[idx[bs.ID]] = bestColor
	}
	return assign
}

// DSATURAssignment: DSATUR'un (Brélaz 1979) K-renkli, ağırlıklı uyarlaması.
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
