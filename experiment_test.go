package main

import (
	"math/rand"
	"testing"
)

// Ölçeklenmiş zamanlayıcılarla uçtan uca duman testi: küçük bir ağ
// hızlandırılmış protokolde de yakınsamalı ve mesaj sayaçları dolmalı.
func TestScaledSimulationConverges(t *testing.T) {
	SetTimeScale(0.05)
	defer SetTimeScale(1.0)

	rng := rand.New(rand.NewSource(5))
	net := BuildNetwork(rng, 12, 300, 100, false)

	convSec, converged := RunSimulation(net, 60*ThinkPeriod)
	if !converged {
		t.Fatalf("ölçek 0.05'te 12 düğüm %d turda yakınsamadı", int(convSec/ThinkPeriod.Seconds()))
	}
	ms := CollectMessageStats(net)
	if ms.Total == 0 || ms.Proposes == 0 {
		t.Errorf("mesaj sayaçları boş: %+v", ms)
	}
	if ms.Dropped > ms.Total/2 {
		t.Errorf("mesajların yarıdan fazlası düştü (%d/%d) — ölçek çok agresif olabilir", ms.Dropped, ms.Total)
	}
}
