package main

import (
	"math/rand"
	"testing"
	"time"
)

// Tüm baseline'lar geçerli atama üretmeli ve maliyet tanımları tutarlı olmalı.
func TestBaselinesConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	net := BuildNetwork(rng, 25, 300, 100, false)

	greedy := GreedyAssignment(net)
	dsatur := DSATURAssignment(net)
	fixed := FixedReuseAssignment(net, MaxColors)
	random := RandomAssignment(net, MaxColors, rng)

	for name, a := range map[string][]PRB{"greedy": greedy, "dsatur": dsatur, "fixed": fixed, "random": random} {
		if len(a) != len(net) {
			t.Fatalf("%s: atama uzunluğu %d != %d", name, len(a), len(net))
		}
		for i, c := range a {
			if c < 0 || c >= MaxColors {
				t.Fatalf("%s: istasyon %d geçersiz renk %d", name, i, c)
			}
		}
	}

	// CalculateGreedyBaseline == AssignmentCost(GreedyAssignment) (wrapper tutarlılığı)
	if got, want := CalculateGreedyBaseline(net), AssignmentCost(net, greedy); got != want {
		t.Errorf("greedy wrapper tutarsız: %.6e != %.6e", got, want)
	}

	// Sıralama beklentisi (aynı graf): akıllı sezgiseller <= gerçek optimum x makul çarpan;
	// en azından DSATUR ve Greedy, rastgele tahsisten kötü olmamalı (bu grafta).
	cd, cg, cr := AssignmentCost(net, dsatur), AssignmentCost(net, greedy), AssignmentCost(net, random)
	if cd > cr || cg > cr {
		t.Errorf("sezgiseller rastgeleden kötü: dsatur=%.3e greedy=%.3e random=%.3e", cd, cg, cr)
	}

	// Hiçbir şema kanıtlanmış optimumun altına inemez.
	opt := BruteForceOptimum(net, MaxColors, 5*time.Second)
	if opt.Exact {
		for name, c := range map[string]float64{"greedy": cg, "dsatur": cd, "random": cr} {
			if c < opt.Cost-1e-18 {
				t.Errorf("%s maliyeti (%.3e) optimumun (%.3e) altında — imkânsız", name, c, opt.Cost)
			}
		}
	}
}
