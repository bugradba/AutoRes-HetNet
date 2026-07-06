package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

//FAZ 2 HABERLEŞME VE ALGORİTMALARR

func NewBaseStation(id Agent_ID, x, y float64) *BaseStation {
	return &BaseStation{
		ID:          id,
		X:           x,
		Y:           y,
		Inbox:       make(chan Message, 100), //Bufer kanalı
		Outbox:      make(map[Agent_ID]chan Message),
		CurrentPRB:  -1,
		Quit:        make(chan struct{}), // Stop() ile kapatılır; yaşam döngüsünü sonlandırır
		State:       STATE_SENSING,
		NeighborMap: make(map[Agent_ID]PRB),
		ProposedPRB: -1,

		NeighborWeights: make(map[Agent_ID]float64), // Haritayı başlatmak için
	}
}

// Start: İstasyonun ana yaşam döngüsünü başlatır.
// wg parametre olarak alınır ki her Monte Carlo koşusu kendi
// WaitGroup'unu kullanabilsin (global durum koşular arasında sızmasın).
func (bs *BaseStation) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	// Rastgele başlangıç gecikmesi (Herkes aynı anda başlamasın)
	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)

	if Verbose {
		fmt.Printf("[BS-%d] Start (Position: %.1f, %.1f) -> Mod: Sensing\n", bs.ID, bs.X, bs.Y)
	}

	// Ticker: Her 500ms de bir "Düşün" (Think)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Komşulara Selam Ver
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
		bs.State = STATE_PROPOSING

	case STATE_PROPOSING:
		// ESKİ: for loop ile boş renk arama
		// YENİ: Utility Maximization (Best Response)
		bestPick := bs.CalculateBestResponse()

		bs.ProposedPRB = bestPick
		if Verbose {
			fmt.Printf("[BS-%d] Utility Maximize Edildi -> Selected Colour: %d\n", bs.ID, bestPick)
		}

		bs.Broadcast(MSG_PROPOSE, "Utility based proposal", bestPick)
		bs.State = STATE_WAITING

		// Timeout (Backoff) logic aynı kalabilir...
		go func(proposed PRB) {
			time.Sleep(2 * time.Second)
			bs.Mutex.Lock()
			defer bs.Mutex.Unlock()
			// Eğer hala aynı teklifteysek ve girişim (Conflict) mesajı gelmediyse kazanmışızdır
			if bs.State == STATE_WAITING && bs.ProposedPRB == proposed {
				bs.State = STATE_COMMITTED
				bs.CurrentPRB = proposed
				// Nash Dengesi: Durumumu değiştirmek için bir sebebim kalmadı.
				if Verbose {
					fmt.Printf(" [BS-%d] NASH EQUILIBRIUM REACHED -> Color %d\n", bs.ID, bs.CurrentPRB)
				}
				bs.Broadcast(MSG_SUCCESS, "Commit", bs.CurrentPRB)
			}
		}(bestPick)

		// Diğer case'ler (WAITING, COMMITTED) aynı kalabilir ...
	}
}

// Oyun Teorisi Mateatiği

// Şöyle çalışır:
// Ajan bakar: "5 rengin hepsi komşularımda var, tamamen boş renk yok."
// Hesap yapar: "Tamam, boş yer yok ama en az zararı hangisi verir?"
// Karar verir: "A cihazı (Color 1) çok yakında, onla aynı frekansı kullanırsam sinyalim ölür.
// Ama Cihaz B (Color 2) bana biraz uzak.
// Mecburen Color 2'yi seçip biraz gürültüye razı olacağım."
// Bu kısımda bunu çözen f(CalculateBestResponse) dur

func (bs *BaseStation) CalculateBestResponse() PRB {

	bestColor := PRB(-1)
	minInterference := math.MaxFloat64

	// Her olası rengi (stratejiyi) dene
	for c := PRB(0); c < MaxColors; c++ {
		currentInterference := 0.0

		// Bu rengi seçersem komşulardan ne kadar "ceza" yerim?
		// Formül: Toplam (Ceza * Ağırlık)
		for neighborID, neighborColor := range bs.NeighborMap {
			if neighborColor == c {
				// Eğer komşum da bu rengi kullanıyorsa, aramızdaki ağırlık kadar ceza ekle
				weight := bs.NeighborWeights[neighborID]
				currentInterference += weight
			}
		}

		// En düşük girişimi sağlayan rengi seç
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

		//Çakışma Kontorlu
		if bs.State == STATE_COMMITTED && bs.CurrentPRB == msg.Value {
			bs.Send(msg.Sender_ID, MSG_CONFLICT, "That colourful!", bs.CurrentPRB)
			return
		}
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			if bs.ID > msg.Sender_ID {
				if Verbose {
					fmt.Printf("⚔ [BS-%d] Conflict! BS-%d'is rejecting (ID Priority).\n", bs.ID, msg.Sender_ID)
				}
				bs.Send(msg.Sender_ID, MSG_CONFLICT, "Wait your turn", bs.CurrentPRB)
			}
		}
		bs.NeighborMap[msg.Sender_ID] = msg.Value
	case MSG_CONFLICT:
		//Itiraz etti.
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			if Verbose {
				fmt.Printf(" [BS-%d] Objection upheld! Withdrawn....\n", bs.ID)
			}
			// Random Backoff ekle:
			time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond) // [cite: 73]
			bs.State = STATE_SENSING
			bs.ProposedPRB = -1
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
		// Non-blocking send: Kanal doluysa bekleme yapma, simülasyonu tıkama
		select {
		case ch <- Message{Sender_ID: bs.ID, Type: msgType, Payload: payload, Value: val}:
		default:
		}
	}
}
