package main

import (
	"testing"
	"time"
)

// ============================================================
// DETERMİNİSTİK PROTOKOL ENTEGRASYON TESTLERİ (G1)
//
// Ajan goroutine'leri BAŞLATILMAZ: mesajlar elle teslim edilir,
// her adımda durum ve giden mesajlar denetlenir. Böylece testler
// zamanlamadan bağımsız ve %100 deterministiktir.
//
// Bu dosyanın varlık nedeni H-1 regresyon garantisidir: WAITING
// durumundaki bir istasyonun gönderdiği CONFLICT, bir zamanlar
// (henüz atanmamış) CurrentPRB'yi, yani -1'i taşıyordu; alıcıdaki
// "ProposedPRB == msg.Value" karşılaştırması hiç tutmuyor ve
// ID-öncelik mekanizması ölü kod haline geliyordu. Buradaki
// testler o hatayı derleme sonrası ilk `go test`te yakalar.
// ============================================================

// twoLinkedStations: elle bağlanmış iki istasyon (A=0, B=1).
func twoLinkedStations(w float64) (*BaseStation, *BaseStation) {
	a := NewBaseStation(0, 0, 0)
	b := NewBaseStation(1, 50, 0)
	a.NeighborWeights[b.ID] = w
	b.NeighborWeights[a.ID] = w
	a.Neighbros = []Agent_ID{b.ID}
	b.Neighbros = []Agent_ID{a.ID}
	a.Outbox[b.ID] = b.Inbox
	b.Outbox[a.ID] = a.Inbox
	return a, b
}

// drain: kanalda bekleyen tek mesajı (varsa) bloklamadan çeker.
func drain(t *testing.T, ch chan Message) (Message, bool) {
	t.Helper()
	select {
	case m := <-ch:
		return m, true
	default:
		return Message{}, false
	}
}

// TestSimultaneousProposalLowerIDWithdraws: G1'in istediği senaryo.
// İki ajan AYNI ANDA aynı rengi teklif etmiş (ikisi de WAITING):
//  1. Yüksek ID (B), düşük ID'nin (A) teklifini görünce CONFLICT
//     göndermeli ve mesaj İTİRAZ EDİLEN RENGİ taşımalı (H-1 çekirdeği).
//  2. Düşük ID (A), yüksek ID'nin teklifine itiraz ETMEMELİ.
//  3. CONFLICT'i alan A geri çekilmeli: SENSING, teklif sıfırlanmış,
//     backoff penceresi kurulmuş (Y-3'ün nextProposeAt karşılığı).
//  4. B teklifinde kalmalı (kazanan).
func TestSimultaneousProposalLowerIDWithdraws(t *testing.T) {
	a, b := twoLinkedStations(1e-9)
	const contested = PRB(2)

	a.State, a.ProposedPRB = STATE_WAITING, contested
	b.State, b.ProposedPRB = STATE_WAITING, contested

	// Adım 1: A'nın teklifi B'ye ulaşır -> B (yüksek ID) itiraz eder.
	b.HandleMessage(Message{Sender_ID: a.ID, Type: MSG_PROPOSE, Value: contested})

	conflict, ok := drain(t, a.Inbox)
	if !ok {
		t.Fatal("yüksek ID'li istasyon çakışan teklife CONFLICT göndermedi")
	}
	if conflict.Type != MSG_CONFLICT {
		t.Fatalf("beklenen MSG_CONFLICT, gelen tip %d", conflict.Type)
	}
	// --- H-1 REGRESYON DENETİMİ ---
	if conflict.Value == -1 {
		t.Fatal("H-1 geri geldi: CONFLICT -1 taşıyor (WAITING istasyonun CurrentPRB'si); itiraz alıcıda sessizce yok sayılır")
	}
	if conflict.Value != contested {
		t.Fatalf("CONFLICT itiraz edilen rengi taşımalı: beklenen %d, gelen %d", contested, conflict.Value)
	}
	if b.State != STATE_WAITING || b.ProposedPRB != contested {
		t.Fatal("itiraz eden (kazanan) B kendi teklifinde kalmalıydı")
	}

	// Adım 2: B'nin teklifi A'ya ulaşır -> A (düşük ID) itiraz ETMEZ.
	a.HandleMessage(Message{Sender_ID: b.ID, Type: MSG_PROPOSE, Value: contested})
	if m, got := drain(t, b.Inbox); got {
		t.Fatalf("düşük ID'li istasyon itiraz etmemeliydi; gönderdi: %+v", m)
	}

	// Adım 3: CONFLICT A'ya teslim edilir -> A geri çekilir.
	before := time.Now()
	a.HandleMessage(conflict)

	if a.State != STATE_SENSING {
		t.Fatalf("düşük ID geri çekilmeliydi: beklenen SENSING, durum %d", a.State)
	}
	if a.ProposedPRB != -1 {
		t.Fatalf("geri çekilen istasyonun teklifi sıfırlanmalı, kalan: %d", a.ProposedPRB)
	}
	if !a.BackoffUntil.After(before) {
		t.Fatal("geri çekilme rastgele backoff penceresi kurmalıydı (BackoffUntil gelecekte değil)")
	}

	// Adım 4: kaybeden, backoff penceresi içinde yeni teklif VERMEMELİ.
	a.Think()
	if a.State != STATE_SENSING {
		t.Fatal("backoff penceresi dolmadan Think() teklif aşamasına geçmemeliydi")
	}

	// Pencere geçmişe alınınca normal akış devam etmeli (SENSING -> PROPOSING).
	a.BackoffUntil = time.Now().Add(-time.Millisecond)
	a.Think()
	if a.State != STATE_PROPOSING {
		t.Fatalf("backoff bitince SENSING -> PROPOSING beklenirdi, durum %d", a.State)
	}
}

// TestStaleMinusOneConflictIsIgnored: H-1'in NEDEN ölü kod ürettiğinin
// belgesi — Value=-1 taşıyan (eski hatalı biçimdeki) bir CONFLICT,
// alıcı tarafında hiçbir etki yaratmamalıdır.
func TestStaleMinusOneConflictIsIgnored(t *testing.T) {
	a, b := twoLinkedStations(1e-9)
	a.State, a.ProposedPRB = STATE_WAITING, PRB(2)

	a.HandleMessage(Message{Sender_ID: b.ID, Type: MSG_CONFLICT, Value: -1})

	if a.State != STATE_WAITING || a.ProposedPRB != PRB(2) {
		t.Fatal("Value=-1 taşıyan CONFLICT teklifi düşürmemeli (eski hatanın ölü-kod kanıtı)")
	}
}

// TestCommittedStationDefendsItsColor: COMMITTED istasyon, kendi
// rengine yapılan teklife CurrentPRB'sini (doğru dolu değer) taşıyan
// CONFLICT ile yanıt vermeli ve teklifi haritasına İŞLEMELİ (Y-4).
func TestCommittedStationDefendsItsColor(t *testing.T) {
	a, b := twoLinkedStations(1e-9)
	b.State, b.CurrentPRB = STATE_COMMITTED, PRB(3)

	b.HandleMessage(Message{Sender_ID: a.ID, Type: MSG_PROPOSE, Value: 3})

	conflict, ok := drain(t, a.Inbox)
	if !ok || conflict.Type != MSG_CONFLICT || conflict.Value != PRB(3) {
		t.Fatalf("COMMITTED istasyon Value=3 taşıyan CONFLICT göndermeliydi, gelen: %+v (ok=%v)", conflict, ok)
	}
	if b.NeighborMap[a.ID] != PRB(3) {
		t.Fatal("itirazdan önce teklif NeighborMap'e işlenmeliydi (Y-4)")
	}
}

// TestInsistWhenContestedColorStillBest: ağırlıklı oyunda girişim
// yasak değil maliyetlidir. İtiraz edenin ağırlığı dahil edildiğinde
// bile itiraz edilen renk TÜM alternatiflerden kesin ucuzsa, istasyon
// teklifinde kalmalıdır — aksi hâlde teklif/itiraz canlı-kilitlenmesi
// doğar (H-2 destek düzeltmesinin regresyon testi).
func TestInsistWhenContestedColorStillBest(t *testing.T) {
	a := NewBaseStation(0, 0, 0)
	// 0,1,3,4 renkleri PAHALI komşularca dolu; itiraz eden 5 numaralı
	// komşunun ağırlığı ihmal edilebilir -> renk 2 hâlâ en iyi yanıt.
	heavy, tiny := 1e-6, 1e-12
	for i, c := range []PRB{0, 1, 3, 4} {
		id := Agent_ID(10 + i)
		a.NeighborWeights[id] = heavy
		a.NeighborMap[id] = c
	}
	objector := Agent_ID(5)
	a.NeighborWeights[objector] = tiny
	a.State, a.ProposedPRB = STATE_WAITING, PRB(2)

	a.HandleMessage(Message{Sender_ID: objector, Type: MSG_CONFLICT, Value: 2})

	if a.State != STATE_WAITING || a.ProposedPRB != PRB(2) {
		t.Fatal("renk 2 hâlâ kesin en ucuzken istasyon geri çekilmemeliydi (livelock koruması)")
	}
	if a.NeighborMap[objector] != PRB(2) {
		t.Fatal("itiraz edenin rengi yine de haritaya işlenmeliydi")
	}
}
