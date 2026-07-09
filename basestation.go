package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// FAZ 2: HABERLEŞME VE ALGORİTMA (ajan yaşam döngüsü)

func NewBaseStation(id Agent_ID, x, y float64) *BaseStation {
	return &BaseStation{
		ID:              id,
		X:               x,
		Y:               y,
		Inbox:           make(chan Message, 100), // tamponlu kanal
		Outbox:          make(map[Agent_ID]chan Message),
		CurrentPRB:      -1,
		Quit:            make(chan struct{}),
		State:           STATE_SENSING,
		NeighborMap:     make(map[Agent_ID]PRB),
		ProposedPRB:     -1,
		NeighborWeights: make(map[Agent_ID]float64),
	}
}

// Start: istasyonun ana yaşam döngüsü. wg parametre olarak alınır ki
// her Monte Carlo koşusu kendi WaitGroup'unu kullanabilsin.
func (bs *BaseStation) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	// Rastgele başlangıç gecikmesi (herkes aynı anda başlamasın)
	time.Sleep(time.Duration(rand.Int63n(int64(StartDelayMax))))

	if Verbose {
		fmt.Printf("[BS-%d] Start (Position: %.1f, %.1f) -> Mod: Sensing\n", bs.ID, bs.X, bs.Y)
	}

	ticker := time.NewTicker(ThinkPeriod)
	defer ticker.Stop()

	// Komşulara selam ver
	bs.Broadcast(MSG_HELLO, "Hi Neighbor!", -1)

	for {
		select {
		case <-bs.Quit: // Stop() çağrıldı: goroutine'i temiz kapat
			return
		case msg := <-bs.Inbox:
			bs.HandleMessage(msg)
		case <-ticker.C:
			bs.Think()
		}
	}
}

// Stop: yaşam döngüsünü sonlandırır. Monte Carlo'da her koşu sonunda
// çağrılmazsa goroutine'ler sızar (200 koşu x 40 istasyon = 8000 sızıntı).
func (bs *BaseStation) Stop() {
	close(bs.Quit)
}

func (bs *BaseStation) Think() {
	bs.Mutex.Lock()
	defer bs.Mutex.Unlock()

	switch bs.State {
	case STATE_SENSING:
		// DÜZELTME (backoff): çakışma sonrası geri çekilme artık burada,
		// kilit tutmadan uyumadan uygulanıyor — süresi dolana kadar
		// yeni teklif verilmez, ama mesaj döngüsü ÇALIŞMAYA DEVAM EDER.
		if time.Now().Before(bs.backoffUntil) {
			return
		}
		bs.State = STATE_PROPOSING

	case STATE_PROPOSING:
		// Utility Maximization (Best Response)
		bestPick := bs.CalculateBestResponse()

		bs.ProposedPRB = bestPick
		bs.proposalSeq++ // yeni teklif: eski commit zamanlayıcıları geçersiz
		seq := bs.proposalSeq

		if Verbose {
			fmt.Printf("[BS-%d] Utility maximize edildi -> Selected Colour: %d\n", bs.ID, bestPick)
		}

		bs.Broadcast(MSG_PROPOSE, "Utility based proposal", bestPick)
		bs.State = STATE_WAITING

		// Commit zaman aşımı: CommitTimeout boyunca itiraz gelmezse kazan.
		// DÜZELTME (bayat zamanlayıcı): yalnızca renk değil, teklif SIRA
		// NUMARASI da eşleşmeli — aksi halde geri çekilip aynı rengi
		// yeniden öneren istasyonu eski zamanlayıcı erken commit eder.
		go func(proposed PRB, seq uint64) {
			time.Sleep(CommitTimeout)
			bs.Mutex.Lock()
			defer bs.Mutex.Unlock()
			if bs.State == STATE_WAITING && bs.ProposedPRB == proposed && bs.proposalSeq == seq {
				bs.State = STATE_COMMITTED
				bs.CurrentPRB = proposed
				if Verbose {
					fmt.Printf(" [BS-%d] NASH EQUILIBRIUM REACHED -> Color %d\n", bs.ID, bs.CurrentPRB)
				}
				bs.Broadcast(MSG_SUCCESS, "Commit", bs.CurrentPRB)
			}
		}(bestPick, seq)
	}
}

// Oyun teorisi çekirdeği: "tamamen boş renk yoksa, komşu ağırlıklarına
// göre EN AZ cezayı getiren rengi seç" (weighted best response).
func (bs *BaseStation) CalculateBestResponse() PRB {
	bestColor := PRB(-1)
	minInterference := math.MaxFloat64

	for c := PRB(0); c < PRB(MaxColors); c++ {
		currentInterference := 0.0
		for neighborID, neighborColor := range bs.NeighborMap {
			if neighborColor == c {
				currentInterference += bs.NeighborWeights[neighborID]
			}
		}
		if currentInterference < minInterference {
			minInterference = currentInterference
			bestColor = c
		}
	}
	return bestColor
}

func (bs *BaseStation) HandleMessage(msg Message) {
	bs.Mutex.Lock()
	defer bs.Mutex.Unlock()

	switch msg.Type {
	case MSG_HELLO:

	case MSG_SUCCESS:
		bs.NeighborMap[msg.Sender_ID] = msg.Value

	case MSG_PROPOSE:
		// Çakışma kontrolü
		if bs.State == STATE_COMMITTED && bs.CurrentPRB == msg.Value {
			bs.Send(msg.Sender_ID, MSG_CONFLICT, "That colour is taken", bs.CurrentPRB)
			bs.NeighborMap[msg.Sender_ID] = msg.Value
			return
		}
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			if bs.ID > msg.Sender_ID {
				if Verbose {
					fmt.Printf("⚔ [BS-%d] Conflict! BS-%d rejected (ID priority).\n", bs.ID, msg.Sender_ID)
				}
				// DÜZELTME (kritik protokol bug'ı): itirazın Value alanı
				// eskiden bs.CurrentPRB idi — WAITING durumunda bu -1'dir,
				// alıcı "ProposedPRB == msg.Value" kontrolünde itirazı
				// SESSİZCE YUTUYORDU ve iki istasyon aynı rengi commit
				// ediyordu. Çekişilen renk ProposedPRB'dir.
				bs.Send(msg.Sender_ID, MSG_CONFLICT, "Wait your turn", bs.ProposedPRB)
			}
		}
		bs.NeighborMap[msg.Sender_ID] = msg.Value

	case MSG_CONFLICT:
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			// İtiraz BİLGİ taşır: gönderen o rengi kullanıyor/istiyor.
			bs.NeighborMap[msg.Sender_ID] = msg.Value

			// DÜZELTME (livelock): en iyi yanıt bu bilgiyle DEĞİŞMEDİYSE
			// teklif korunur — ağırlıklı oyunda çakışma bazen denge
			// maliyetinin parçasıdır (bütün renkler doluysa min-maliyetli
			// renk yine budur). Koşulsuz geri çekilme, boş rengi olmayan
			// istasyonu sonsuza dek renksiz bırakır: commit etmiş sahip
			// her teklife itiraz eder, istasyon asla COMMITTED olamaz.
			if bs.CalculateBestResponse() == bs.ProposedPRB {
				if Verbose {
					fmt.Printf(" [BS-%d] Objection noted; colour %d is still my best response, keeping proposal.\n", bs.ID, bs.ProposedPRB)
				}
				return
			}

			// En iyi yanıt değişti: geri çekil, rastgele backoff ile
			// SENSING'e dön (eşzamanlı yarışlarda livelock kırıcı).
			if Verbose {
				fmt.Printf(" [BS-%d] Objection upheld! Withdrawn...\n", bs.ID)
			}
			bs.State = STATE_SENSING
			bs.ProposedPRB = -1
			bs.proposalSeq++ // bekleyen commit zamanlayıcısını geçersiz kıl
			bs.backoffUntil = time.Now().Add(time.Duration(rand.Int63n(int64(BackoffMax))))
		}
	}
}

func (bs *BaseStation) Broadcast(msgType MessageType, payload string, val PRB) {
	for _, neighborID := range bs.Neighbros {
		bs.Send(neighborID, msgType, payload, val)
	}
}

func (bs *BaseStation) Send(targetID Agent_ID, msgType MessageType, payload string, val PRB) {
	if ch, ok := bs.Outbox[targetID]; ok {
		// Non-blocking send: kanal doluysa bekleme, simülasyonu tıkama.
		// Enstrümantasyon: gönderilen ve DÜŞEN mesajlar sayılır — mesaj
		// karmaşıklığı, dağıtık protokolün asıl maliyet metriğidir.
		select {
		case ch <- Message{Sender_ID: bs.ID, Type: msgType, Payload: payload, Value: val}:
			atomic.AddInt64(&bs.MsgSent[msgType], 1)
		default:
			atomic.AddInt64(&bs.MsgDropped, 1)
		}
	}
}
