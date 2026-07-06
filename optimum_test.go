package main

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// Saf (naive) tam tarama: K^N atamanın hepsini dener. Yalnızca test için.
func naiveOptimum(network []*BaseStation, k int) float64 {
	n := len(network)
	idx := make(map[Agent_ID]int, n)
	for i, bs := range network {
		idx[bs.ID] = i
	}
	colors := make([]int, n)
	best := math.MaxFloat64

	var rec func(pos int)
	rec = func(pos int) {
		if pos == n {
			cost := 0.0
			for i, bs := range network {
				for nid, w := range bs.NeighborWeights {
					j := idx[nid]
					if j > i && colors[i] == colors[j] { // her kenar bir kez
						cost += w
					}
				}
			}
			if cost < best {
				best = cost
			}
			return
		}
		for c := 0; c < k; c++ {
			colors[pos] = c
			rec(pos + 1)
		}
	}
	rec(0)
	return best
}

func randomNetwork(rng *rand.Rand, n int, edgeProb float64) []*BaseStation {
	net := make([]*BaseStation, n)
	for i := 0; i < n; i++ {
		net[i] = NewBaseStation(Agent_ID(i), rng.Float64()*100, rng.Float64()*100)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if rng.Float64() < edgeProb {
				w := rng.Float64() * 1e-8
				net[i].NeighborWeights[net[j].ID] = w
				net[j].NeighborWeights[net[i].ID] = w
			}
		}
	}
	return net
}

func TestBruteForceOptimumMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 30; trial++ {
		n := 4 + rng.Intn(6)         // 4-9 düğüm
		k := 2 + rng.Intn(3)         // 2-4 renk
		p := 0.3 + rng.Float64()*0.6 // seyrekten yoğuna
		net := randomNetwork(rng, n, p)

		want := naiveOptimum(net, k)
		got := BruteForceOptimum(net, k, 5*time.Second)

		if !got.Exact {
			t.Fatalf("trial %d: küçük grafta bütçe aşıldı (n=%d k=%d)", trial, n, k)
		}
		if math.Abs(got.Cost-want) > 1e-15*math.Max(1, want) {
			t.Errorf("trial %d: n=%d k=%d p=%.2f: B&B=%.12e, naive=%.12e", trial, n, k, p, got.Cost, want)
		}
	}
}

func TestOptimumZeroWhenColorable(t *testing.T) {
	// Üçgen, K=3 => uygun renklenebilir => optimum 0 olmalı
	rng := rand.New(rand.NewSource(1))
	net := randomNetwork(rng, 3, 0) // kenarsız başla
	w := 1e-9
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 3; j++ {
			net[i].NeighborWeights[net[j].ID] = w
			net[j].NeighborWeights[net[i].ID] = w
		}
	}
	res := BruteForceOptimum(net, 3, time.Second)
	if !res.Exact || res.Cost != 0 {
		t.Errorf("üçgen K=3: beklenen 0/exact, alınan %.3e exact=%v", res.Cost, res.Exact)
	}
	// Üçgen, K=2 => en az bir kenar çakışmak zorunda => optimum = w
	res2 := BruteForceOptimum(net, 2, time.Second)
	if !res2.Exact || math.Abs(res2.Cost-w) > 1e-24 {
		t.Errorf("üçgen K=2: beklenen %.3e, alınan %.3e exact=%v", w, res2.Cost, res2.Exact)
	}
}
