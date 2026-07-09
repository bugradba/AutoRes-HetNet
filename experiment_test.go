package main

import (
	"math/rand"
	"testing"
)

// Ölçekli zamanlayıcı duman testi: 0.05 ölçekte küçük bir ağ hâlâ
// yakınsamalı, mesaj sayaçları dolmalı ve kuyruk davranışı makul kalmalı.
// (-race altında da temiz çalışması beklenir.)
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

// Yeni protokol düzeltmesinin regresyon testi: iki komşu istasyon aynı
// rengi aynı anda isteyince, CONFLICT mesajının Value alanı ÇEKİŞİLEN
// rengi taşımalı (eski kod -1 gönderiyor, itiraz yutulyordu).
func TestConflictCarriesContestedColor(t *testing.T) {
	net := makePair(10)
	a, b := net[0], net[1]
	a.Outbox[b.ID] = b.Inbox
	b.Outbox[a.ID] = a.Inbox

	// b (yüksek ID) WAITING durumunda 3 rengini önermiş olsun.
	b.State = STATE_WAITING
	b.ProposedPRB = 3

	// a'dan aynı renk için PROPOSE gelsin: b itiraz etmeli.
	b.HandleMessage(Message{Sender_ID: a.ID, Type: MSG_PROPOSE, Value: 3})

	select {
	case msg := <-a.Inbox:
		if msg.Type != MSG_CONFLICT {
			t.Fatalf("CONFLICT beklenirdi, %v geldi", msg.Type)
		}
		if msg.Value != 3 {
			t.Fatalf("CONFLICT çekişilen rengi taşımalı: Value=%d (3 beklenirdi)", msg.Value)
		}
	default:
		t.Fatal("yüksek ID'li WAITING istasyon itiraz göndermedi")
	}

	// a itirazı işleyince teklifini geri çekmeli.
	a.State = STATE_WAITING
	a.ProposedPRB = 3
	a.HandleMessage(Message{Sender_ID: b.ID, Type: MSG_CONFLICT, Value: 3})
	if a.State != STATE_SENSING || a.ProposedPRB != -1 {
		t.Fatalf("itiraz sonrası geri çekilme olmadı: state=%v proposed=%d", a.State, a.ProposedPRB)
	}
}
