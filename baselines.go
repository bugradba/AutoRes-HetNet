package main

import (
	"math"
	"math/rand"
	"sort"
)

// ============================================================
// Y-1 DÜZELTMESİ: BASELINE TAHSİSÇİLERİ
//
// README'nin belgeleyip depoda bulunmayan dosya. Dağıtık çözümün
// karşılaştırıldığı dört referans şema burada yaşar:
//
//   - Greedy   : ağırlıklı zorluk sırasına göre merkezi açgözlü
//   - DSATUR   : klasik doygunluk-derecesi sezgiseli (ağırlık duyarlı
//                renk seçimiyle)
//   - Fixed    : statik frekans yeniden kullanımı (renk = ID mod K)
//   - Random   : tekdüze rastgele tahsis (alt sınır / kontrol)
//
// ORTAK MALİYET TANIMI: Tüm şemalar (dağıtık NE dahil) aynı maliyetle
// ölçülür — AssignmentCost: aynı renkli komşu çiftlerinin kenar
// ağırlıkları toplamı, her kenar bir kez. CalculateGlobalObjective
// ve BruteForceOptimum ile birebir aynı tanımdır.
// ============================================================

// ColorsOfNetwork: ağın mevcut (dağıtık protokolün ürettiği) renk
// haritası. FAILED istasyonlar -1 olarak kalır.
func ColorsOfNetwork(network []*BaseStation) map[Agent_ID]PRB {
	colors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		colors[bs.ID] = bs.CurrentPRB
	}
	return colors
}

// AssignmentCost: verilen tahsisin toplam çakışma maliyeti.
// Renk atanmamış (-1) istasyonlar iletmez, maliyete katılmaz.
func AssignmentCost(network []*BaseStation, colors map[Agent_ID]PRB) float64 {
	total := 0.0
	for _, bs := range network {
		myColor, ok := colors[bs.ID]
		if !ok || myColor == -1 {
			continue
		}
		for neighborID, weight := range bs.NeighborWeights {
			if nc, ok := colors[neighborID]; ok && nc != -1 && nc == myColor {
				total += weight
			}
		}
	}
	return total / 2.0 // her kenar iki kez sayıldı
}

// minConflictColor: verilen kısmi renklendirmede bs için ağırlıklı
// çakışması en düşük rengi döndürür (eşitlikte en düşük indeks).
func minConflictColor(bs *BaseStation, colors map[Agent_ID]PRB) PRB {
	bestColor := PRB(0)
	minImpact := math.MaxFloat64
	for c := PRB(0); c < MaxColors; c++ {
		impact := 0.0
		for neighborID, weight := range bs.NeighborWeights {
			if nc, ok := colors[neighborID]; ok && nc != -1 && nc == c {
				impact += weight
			}
		}
		if impact < minImpact {
			minImpact = impact
			bestColor = c
		}
	}
	return bestColor
}

// GreedyAssignment: istasyonları "zorluk derecesine" (toplam komşu
// ağırlığı) göre büyükten küçüğe sıralar, her birine o an en az ceza
// getiren rengi atar. (metrics.go'daki CalculateGreedyBaseline bu
// fonksiyonun maliyet sarmalayıcısıdır.)
func GreedyAssignment(network []*BaseStation) map[Agent_ID]PRB {
	sorted := make([]*BaseStation, len(network))
	copy(sorted, network)
	weightOf := func(bs *BaseStation) float64 {
		w := 0.0
		for _, v := range bs.NeighborWeights {
			w += v
		}
		return w
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return weightOf(sorted[i]) > weightOf(sorted[j])
	})

	colors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		colors[bs.ID] = -1
	}
	for _, bs := range sorted {
		colors[bs.ID] = minConflictColor(bs, colors)
	}
	return colors
}

// DSATURAssignment: klasik DSATUR — her adımda doygunluk derecesi
// (renklenmiş komşulardaki FARKLI renk sayısı) en yüksek düğümü seç,
// eşitliği ağırlıklı dereceyle boz; renk seçimi ağırlık duyarlıdır
// (en az çakışma ağırlığı, eşitlikte en düşük indeks — K yetiyorsa
// bu, klasik "en küçük uygun renk" kuralına indirger).
func DSATURAssignment(network []*BaseStation) map[Agent_ID]PRB {
	colors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		colors[bs.ID] = -1
	}

	weightOf := func(bs *BaseStation) float64 {
		w := 0.0
		for _, v := range bs.NeighborWeights {
			w += v
		}
		return w
	}
	saturation := func(bs *BaseStation) int {
		seen := make(map[PRB]bool)
		for neighborID := range bs.NeighborWeights {
			if nc := colors[neighborID]; nc != -1 {
				seen[nc] = true
			}
		}
		return len(seen)
	}

	for assigned := 0; assigned < len(network); assigned++ {
		var pick *BaseStation
		bestSat, bestW := -1, -1.0
		for _, bs := range network {
			if colors[bs.ID] != -1 {
				continue
			}
			sat, w := saturation(bs), weightOf(bs)
			if sat > bestSat || (sat == bestSat && w > bestW) {
				pick, bestSat, bestW = bs, sat, w
			}
		}
		colors[pick.ID] = minConflictColor(pick, colors)
	}
	return colors
}

// FixedReuseAssignment: statik frekans planlaması — renk = ID mod K.
// Topolojiden habersizdir; klasik hücresel yeniden kullanımın naive hali.
func FixedReuseAssignment(network []*BaseStation) map[Agent_ID]PRB {
	colors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		colors[bs.ID] = PRB(int(bs.ID) % int(MaxColors))
	}
	return colors
}

// RandomAssignment: tekdüze rastgele tahsis (kontrol / alt sınır).
// Deney rng'sinden beslenir; aynı seed aynı tahsisi üretir.
func RandomAssignment(network []*BaseStation, rng *rand.Rand) map[Agent_ID]PRB {
	colors := make(map[Agent_ID]PRB, len(network))
	for _, bs := range network {
		colors[bs.ID] = PRB(rng.Intn(int(MaxColors)))
	}
	return colors
}
