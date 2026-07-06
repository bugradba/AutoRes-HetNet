package main

import (
	"math"
	"sort"
	"time"
)

// GERÇEK SOSYAL OPTİMUM HESAPLAYICISI (Exact / Branch-and-Bound)
//
// PoA tanımı gereği paydada GERÇEK optimum bulunmalıdır:
// PoA = (en kötü Nash dengesi maliyeti) / (sosyal optimum maliyeti)
// Bu dosya, ağırlıklı graf renklendirme probleminin kesin (exact)
// minimum maliyetini bulur. Yöntem:
//
//  1. Girişim grafını bağlı bileşenlere (connected components) ayır —
//      her bileşen bağımsız çözülebilir, arama uzayı çarpım yerine toplam olur.
//
//   2. Her bileşende branch-and-bound: düğümleri dereceye göre sırala,
//      kısmi maliyet o ana kadarki en iyi çözümü aştığı an dalı buda.
//
//   3. Simetri kırma: renkler birbirinin yerine geçebilir (renk 0 ile
//      renk 3'ün takası maliyeti değiştirmez); bu yüzden yeni bir düğüme
//      en fazla "o ana dek kullanılan renk sayısı + 1" indeksli renk denenir.
//
//   4. Zaman bütçesi: bütçe aşılırsa Exact=false döner ve sonuç
//      "kanıtlanmış optimum" olarak DEĞİL, yalnızca üst sınır olarak raporlanmalıdır.
//
// Maliyet tanımı CalculateGlobalObjective ile aynıdır: aynı renkli komşu
// çiftlerinin kenar ağırlıkları toplamı (her kenar bir kez sayılır).

type OptimumResult struct {
	Cost  float64 // bulunan en iyi (minimum) toplam çakışma maliyeti
	Exact bool    // true: kanıtlanmış optimum; false: zaman bütçesi aşıldı (üst sınır)
}

type edge struct {
	to int
	w  float64
}

func BruteForceOptimum(network []*BaseStation, k int, budget time.Duration) OptimumResult {
	deadline := time.Now().Add(budget)

	// Agent_ID -> dizi indeksi eşlemesi
	idx := make(map[Agent_ID]int, len(network))
	for i, bs := range network {
		idx[bs.ID] = i
	}

	// Komşuluk listesi (her iki yönde kayıtlı; maliyette tek yön kullanılacak)
	n := len(network)
	adj := make([][]edge, n)
	for i, bs := range network {
		for nid, w := range bs.NeighborWeights {
			adj[i] = append(adj[i], edge{to: idx[nid], w: w})
		}
	}

	// Bağlı bileşenleri bul (izole düğümler maliyete katkı yapmaz, atlanır)
	compID := make([]int, n)
	for i := range compID {
		compID[i] = -1
	}
	var components [][]int
	for s := 0; s < n; s++ {
		if compID[s] != -1 || len(adj[s]) == 0 {
			continue
		}
		c := len(components)
		stack := []int{s}
		compID[s] = c
		var nodes []int
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			nodes = append(nodes, v)
			for _, e := range adj[v] {
				if compID[e.to] == -1 {
					compID[e.to] = c
					stack = append(stack, e.to)
				}
			}
		}
		components = append(components, nodes)
	}

	total := 0.0
	exact := true
	for _, nodes := range components {
		cost, ok := solveComponent(nodes, adj, k, deadline)
		total += cost
		if !ok {
			exact = false
		}
	}
	return OptimumResult{Cost: total, Exact: exact}
}

// solveComponent: tek bir bağlı bileşen için branch-and-bound.
// Dönen bool: deadline aşılmadan optimum kanıtlandı mı.
func solveComponent(nodes []int, adj [][]edge, k int, deadline time.Time) (float64, bool) {
	m := len(nodes)

	// Global indeks -> bileşen-yerel indeks
	local := make(map[int]int, m)
	// Dereceye (toplam komşu ağırlığına) göre büyükten küçüğe sırala:
	// zor düğümler önce dallanırsa budama çok daha erken devreye girer.
	order := make([]int, m)
	copy(order, nodes)
	weightSum := func(v int) float64 {
		s := 0.0
		for _, e := range adj[v] {
			s += e.w
		}
		return s
	}
	sort.Slice(order, func(a, b int) bool { return weightSum(order[a]) > weightSum(order[b]) })
	for pos, v := range order {
		local[v] = pos
	}

	// Yalnızca bileşen içi ve "önce atanmış" komşulara bakan kenar listesi:
	// pos konumundaki düğümün, kendisinden önce renklendirilen komşuları.
	prevEdges := make([][]edge, m)
	for pos, v := range order {
		for _, e := range adj[v] {
			if lp, ok := local[e.to]; ok && lp < pos {
				prevEdges[pos] = append(prevEdges[pos], edge{to: lp, w: e.w})
			}
		}
	}

	colors := make([]int, m)
	for i := range colors {
		colors[i] = -1
	}
	best := math.MaxFloat64
	proven := true
	checkCounter := 0

	var dfs func(pos int, cost float64, usedColors int)
	dfs = func(pos int, cost float64, usedColors int) {
		// Zaman bütçesi kontrolü (her 1024 düğüm genişletmesinde bir saat oku)
		checkCounter++
		if checkCounter&1023 == 0 && time.Now().After(deadline) {
			proven = false
			return
		}
		if !proven || best == 0 { // bütçe bitti ya da 0 maliyet bulundu: daha iyisi imkânsız
			return
		}
		if pos == m {
			if cost < best {
				best = cost
			}
			return
		}
		// Simetri kırma: en fazla usedColors+1 farklı renk dene (k'yi aşmadan)
		limit := usedColors + 1
		if limit > k {
			limit = k
		}
		for c := 0; c < limit; c++ {
			add := 0.0
			for _, e := range prevEdges[pos] {
				if colors[e.to] == c {
					add += e.w
				}
			}
			newCost := cost + add
			if newCost >= best { // BUDAMA: bu dal en iyiyi geçemez
				continue
			}
			colors[pos] = c
			next := usedColors
			if c == usedColors {
				next++
			}
			dfs(pos+1, newCost, next)
			colors[pos] = -1
		}
	}

	dfs(0, 0.0, 0)
	if best == math.MaxFloat64 {
		// Hiç tam çözüme ulaşılamadı (yalnızca bütçe çok küçükse olur)
		return 0, false
	}
	return best, proven
}
