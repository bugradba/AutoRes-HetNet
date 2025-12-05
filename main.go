package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"
)

type Agent_ID int

type PRB int

type MessageType int

type AgentState int

const (
	MSG_HELLO MessageType = iota // İOTA go da ardışık sabit değerler için kullanılan özel bir sabit oluşturucusudur
	MSG_PROPOSE
	MSG_SUCCESS
	MSG_CONFLICT
)
const (
	STATE_SENSING AgentState = iota
	STATE_PROPOSING
	STATE_WAITING
	STATE_COMMITTED
)

type Message struct {
	Sender_ID Agent_ID
	Type      MessageType
	Payload   string
	Value     PRB
}

type BaseStation struct {
	ID        Agent_ID
	X, Y      float64 // Şimdilik konum ve mesafe hesabı için
	Neighbros []Agent_ID
	Inbox     chan Message
	Outbox    map[Agent_ID]chan Message
	Isactive  bool // Aktiflik Kontorlu için

	//-----------
	State       AgentState       //Şu an Ne yapıyor
	CurrentPRB  PRB              //Kazandığı Renk
	ProposedPRB PRB              //İstegiği renk
	NeighborMap map[Agent_ID]PRB // Komşuların renklerini tuttuğu hafıza

	Mutex sync.Mutex
}

//GLOBAL DÜNYA ORTMAI YAZICAĞUIZ
//Bu kısım gerçek dağıtkı sitemlerde olmaz ama biz similasyon yaptığımız için

var Network []*BaseStation

var wg sync.WaitGroup

//FAZ 2 HABERLEŞME VE ALGORİTMALARR

func NewBaseStation(id Agent_ID, x, y float64) *BaseStation {
	return &BaseStation{
		ID:          id,
		X:           x,
		Y:           y,
		Inbox:       make(chan Message, 100), //Bufer kanalı
		Outbox:      make(map[Agent_ID]chan Message),
		CurrentPRB:  -1,
		Isactive:    true,
		State:       STATE_SENSING,
		NeighborMap: make(map[Agent_ID]PRB),
		ProposedPRB: -1,
	}
}

// Strat: İstasyonun ana yşama döngüsünü başlatır
func (bs *BaseStation) Start() {
	defer wg.Done()

	// Rastgele başlangıç gecikmesi (Herkes aynı anda başlamasın)
	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)

	fmt.Printf("[BS-%d] Start (Position: %.1f, %.1f) -> Mod: Sensing\n", bs.ID, bs.X, bs.Y)

	// Ticker: Her 500ms de bir "Düşün" (Think)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Komşulara Selam Ver
	bs.Broadcast(MSG_HELLO, "Hi Neighbor!", -1)

	for bs.Isactive {
		select {
		case msg := <-bs.Inbox:
			bs.HandleMessage(msg)
		case <-ticker.C:
			bs.Think()
		}
	}
}

func (bs *BaseStation) Think() {
	bs.Mutex.Lock()
	defer bs.Mutex.Unlock()

	switch bs.State {
	case STATE_SENSING:
		// Algoritmayı başlat
		bs.State = STATE_PROPOSING

	case STATE_PROPOSING:
		candidate := PRB(0)
		for {
			isTaken := false
			for _, usedColor := range bs.NeighborMap {
				if usedColor == candidate {
					isTaken = true
					break
				}
			}

			// Eğer hiçbir komşuda bu renk yoksa, bulduk demektir
			if !isTaken {
				break
			}

			candidate++
		}

		// Teklif Kısmı
		bs.ProposedPRB = candidate
		fmt.Printf("[BS-%d] Proposing Color %d...\n", bs.ID, candidate)
		bs.Broadcast(MSG_PROPOSE, "I Want this Color", candidate)

		// Bekleme Moduna Geç
		bs.State = STATE_WAITING

		// Asenkron olarak sonucu kontrol et (timeout süresi kadar bekle)
		go func(proposed PRB) {
			time.Sleep(2 * time.Second)

			bs.Mutex.Lock()
			defer bs.Mutex.Unlock()
			if bs.State == STATE_WAITING && bs.ProposedPRB == proposed {
				bs.State = STATE_COMMITTED
				bs.CurrentPRB = proposed
				fmt.Printf(" [BS-%d] SUCCESS! Committed to PRB-%d.\n", bs.ID, bs.CurrentPRB)
				bs.Broadcast(MSG_SUCCESS, "I took the color", bs.CurrentPRB)
			}
		}(candidate)

	case STATE_WAITING:
		// Pasif bekleme modu, bir şey yapma.

	case STATE_COMMITTED:
		// Zaten rengi aldım, keyfine bak.

	}
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
				fmt.Printf("⚔️ [BS-%d] Conflict! BS-%d'is rejecting (ID Priority).\n", bs.ID, msg.Sender_ID)
				bs.Send(msg.Sender_ID, MSG_CONFLICT, "Wait your turn", bs.CurrentPRB)
			}
		}
		bs.NeighborMap[msg.Sender_ID] = msg.Value
	case MSG_CONFLICT:
		//Itiraz etti.
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			fmt.Printf(" [BS-%d] Objection upheld! Withdrawn....\n", bs.ID)
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

// ------------- Yardımcı olacak fonksiyonlarr -------------

func Distance(a, b *BaseStation) float64 { //MESAFE HESABI
	return math.Sqrt(math.Pow(a.X-b.X, 2) + math.Pow(a.Y-b.Y, 2))
}

func main() {
	rand.Seed(time.Now().UnixNano())

	numDevice := 20   // İstasyon sayısı
	areaSize := 200.0 // Alan boyutu
	threshold := 45.0 // Komşuluk mesafesi

	fmt.Println("--- 5G Distributed Resource Management Simulation Commences ---")

	//İstasyonları Oluştur
	Network = make([]*BaseStation, numDevice)
	for i := 0; i < numDevice; i++ {
		Network[i] = NewBaseStation(Agent_ID(i), rand.Float64()*areaSize, rand.Float64()*areaSize)
	}

	//Topolojiyi Kur
	for i := 0; i < numDevice; i++ {
		for j := i + 1; j < numDevice; j++ {
			dist := Distance(Network[i], Network[j])
			if dist < threshold {
				// Komşuluk Listesine Ekle
				Network[i].Neighbros = append(Network[i].Neighbros, Network[j].ID)
				Network[j].Neighbros = append(Network[j].Neighbros, Network[i].ID)
				// Sanal Kabloları Bağla
				Network[i].Outbox[Network[j].ID] = Network[j].Inbox
				Network[j].Outbox[Network[i].ID] = Network[i].Inbox
				fmt.Printf("Connection: BS-%d <--> BS-%d (Distance: %.2fm)\n", i, j, dist)
			}
		}
	}

	wg.Add(numDevice)
	for _, bs := range Network {
		go bs.Start()
	}

	time.Sleep(15 * time.Second)

	fmt.Println("\n--- Simulation completed ---")
	fmt.Println("--- FINAL RESULTS ---")

	for _, bs := range Network {
		status := "FAILED"
		if bs.State == STATE_COMMITTED {
			status = fmt.Sprintf(" PRB-%d", bs.CurrentPRB)
		}
		fmt.Printf("BS-%d: %s (Neighbors: %d)\n", bs.ID, status, len(bs.Neighbros))
	}
	// ... Main fonksiyonunun içi, loop bittikten sonra ...

	// Görselleştirme için veri yapısı
	type VizData struct {
		Nodes []struct {
			ID    int     `json:"id"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Color int     `json:"color"`
		} `json:"nodes"`
		Edges []struct {
			Source int `json:"source"`
			Target int `json:"target"`
		} `json:"edges"`
	}

	data := VizData{}
	for _, bs := range Network {
		// Node ekle
		data.Nodes = append(data.Nodes, struct {
			ID    int     `json:"id"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Color int     `json:"color"`
		}{
			ID:    int(bs.ID),
			X:     bs.X,
			Y:     bs.Y,
			Color: int(bs.CurrentPRB),
		})

		for _, neighborID := range bs.Neighbros {
			if bs.ID < neighborID {
				data.Edges = append(data.Edges, struct {
					Source int `json:"source"`
					Target int `json:"target"`
				}{
					Source: int(bs.ID),
					Target: int(neighborID),
				})
			}
		}
	}

	// Dosyaya yaz
	file, _ := json.MarshalIndent(data, "", " ")
	_ = os.WriteFile("viz_data.json", file, 0644)
	fmt.Println(" Veriler 'viz_data.json' dosyasına kaydedildi.")
}
