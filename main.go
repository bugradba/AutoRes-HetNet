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

// --- TELEKOMÜNİKASYON FİZİK SABİTLERİ ---
const (
	TxPowerWatts     = 40.0  // 46 dBm
	NoiseWatts       = 1e-13 // -100 dBm (Biraz daha hassas)
	BandwidthHz      = 20e6  // 20 MHz
	ReferenceLoss    = 1e-4  // Sinyalin ilk metresindeki kayıp (-40dB)
	PathLossExponent = 3.0   // Şehir içi ortam için sönümleme katsayısı
)

type Message struct {
	Sender_ID Agent_ID
	Type      MessageType
	Payload   string
	Value     PRB
}

type BaseStation struct {
	ID              Agent_ID
	X, Y            float64              // Şimdilik konum ve mesafe hesabı için
	NeighborWeights map[Agent_ID]float64 // Ağırlıklı Girişim Grafiği
	Inbox           chan Message
	Outbox          map[Agent_ID]chan Message
	Neighbros       []Agent_ID
	Isactive        bool
	State           AgentState       //Şu an Ne yapıyor
	CurrentPRB      PRB              //Kazandığı Renk
	ProposedPRB     PRB              //İstegiği renk
	NeighborMap     map[Agent_ID]PRB // Komşuların renklerini tuttuğu hafıza
	Throughput      float64          // Mbps cinsinden veri hızı
	Mutex           sync.Mutex
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

		NeighborWeights: make(map[Agent_ID]float64), // Haritayı başlatmak için
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
		bs.State = STATE_PROPOSING

	case STATE_PROPOSING:
		// ESKİ: for loop ile boş renk arama
		// YENİ: Utility Maximization (Best Response)
		bestPick := bs.CalculateBestResponse()

		bs.ProposedPRB = bestPick
		fmt.Printf("[BS-%d] Utility Maximize Edildi -> Selected Colour: %d\n", bs.ID, bestPick)

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
				fmt.Printf(" [BS-%d] NASH EQUILIBRIUM REACHED -> Color %d\n", bs.ID, bs.CurrentPRB)
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

	const MaxColors = 5

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
				fmt.Printf("⚔ [BS-%d] Conflict! BS-%d'is rejecting (ID Priority).\n", bs.ID, msg.Sender_ID)
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

//  Jain's Fairness Index Fonksiyonu
// Formül: (Toplam x)^2 / (n * Toplam x^2)

func CalculateJainsFairness(network []*BaseStation) float64 {
	var sumThroughput float64
	var sumSquareThroughput float64
	n := float64(len(network))

	for _, bs := range network {
		xi := bs.Throughput
		sumThroughput += xi
		sumSquareThroughput += (xi * xi)
	}

	if sumSquareThroughput == 0 {
		return 0
	}

	jainsIndex := (sumThroughput * sumThroughput) / (n * sumSquareThroughput)
	return jainsIndex
}

// GLOBAL AMAÇ FONKSİYONU (Total Network Interference)
// Tüm ağdaki toplam çatışma maliyetini hesaplar.
// Hedef: Bu değerin simülasyon sonunda azalmış olmasıdır.

func CalculateGlobalObjective(network []*BaseStation) float64 {
	totalCost := 0.0

	for _, bs := range network {
		if bs.CurrentPRB == -1 {
			continue
		}

		for neighborID, weight := range bs.NeighborWeights {
			var neighborColor PRB = -1
			for _, node := range network {
				if node.ID == neighborID {
					neighborColor = node.CurrentPRB
					break
				}
			}

			if neighborColor != -1 && bs.CurrentPRB == neighborColor {
				totalCost += weight
			}
		}
	}

	return totalCost / 2.0
}

//  ANARŞİNİN BEDELİ (PoA) HESAPLAYICISI

// Bu fonksiyon "Merkezi Bir Süper Bilgisayar" gibi davranır.
// Tüm ağı kuşbakışı görür ve renkleri en verimli şekilde dağıtır (Centralized Greedy).

// İstasyonları "Zorluk Derecesine" göre sırala (Node Degree / Weight)
// En çok komşusu olan (en zor) istasyona önce renk verirsek, global optimuma yaklaşırız.
// (Basit bir Bubble Sort yapıyoruz, node sayısı az olduğu için yeterli)

func CalculateCentralizedOptimum(network []*BaseStation) float64 {
	// Mevcut simülasyonu bozmamak için geçici bir renk haritası oluşturuyoruz
	tempColors := make(map[Agent_ID]PRB)
	for _, bs := range network {
		tempColors[bs.ID] = -1 // Önce herkes renksiz
	}

	sortedNodes := make([]*BaseStation, len(network))
	copy(sortedNodes, network)

	for i := 0; i < len(sortedNodes); i++ {
		for j := 0; j < len(sortedNodes)-i-1; j++ {
			// Komşu sayısı * Ağırlık toplamı mantığıyla "zorluk" ölçelim
			weightI := 0.0
			for _, w := range sortedNodes[j].NeighborWeights {
				weightI += w
			}

			weightJ := 0.0
			for _, w := range sortedNodes[j+1].NeighborWeights {
				weightJ += w
			}

			if weightI < weightJ { // Büyükten küçüğe sırala
				sortedNodes[j], sortedNodes[j+1] = sortedNodes[j+1], sortedNodes[j]
			}
		}
	}

	// Merkezi Zeka ile Renk Dağıt (Greedy Optimization)
	const MaxColors = 5 //simülasyon sayısı ile aynı

	for _, bs := range sortedNodes {
		bestColor := PRB(0)
		minGlobalImpact := math.MaxFloat64

		for c := PRB(0); c < MaxColors; c++ {
			currentImpact := 0.0

			for neighborID, weight := range bs.NeighborWeights {
				if assignedColor, exists := tempColors[neighborID]; exists && assignedColor != -1 {
					if assignedColor == c {
						currentImpact += weight
					}
				}
			}

			if currentImpact < minGlobalImpact {
				minGlobalImpact = currentImpact
				bestColor = c
			}
		}
		tempColors[bs.ID] = bestColor
	}

	// Bu mükemmel dağıtımın toplam maliyetini hesapla
	totalCentralizedCost := 0.0
	for _, bs := range network {
		myColor := tempColors[bs.ID]
		for neighborID, weight := range bs.NeighborWeights {
			neighborColor := tempColors[neighborID]
			if myColor == neighborColor {
				totalCentralizedCost += weight
			}
		}
	}

	return totalCentralizedCost / 2.0
}

// SINR ve SHANNON KAPASİTE HESABI
// 1. Sinyal Gücü (Signal Power - P_i * h_ii)
// Kendi kullanıcımız bize "UserDistance" kadar uzakta varsayıyoruz.
// Ters Kare Yasası + Shadowing (Ortalama 1.0 kabul edelim kendimiz için)
// Girişim Gücü (Interference Power - Sum(P_j * h_ji))
// Sadece "Aynı Rengi" kullanan komşulardan gelen gürültüyü topla
// SINR Hesabı (Signal / (Interference + Noise))
// Shannon Kapasitesi (C = B * log2(1 + SINR))

// Sinyal Kazancı (Path Loss) Hesabı: 1 / d^2 (Basit Serbest Uzay Modeli)
// Mesafe (actualUserDist) arttıkça, payda büyür ve kazanç düşer.

func (bs *BaseStation) CalculateShannonCapacity(network []*BaseStation) {

	// Her istasyon için 10 metre ile 300 metre arasında rastgele bir kullanıcı mesafesi belirleyelim.
	minDist := 10.0
	maxDist := 300.0

	rSource := rand.NewSource(time.Now().UnixNano() + int64(bs.ID)*100)
	rGen := rand.New(rSource)

	actualUserDist := minDist + rGen.Float64()*(maxDist-minDist)

	signalGain := ReferenceLoss * math.Pow(actualUserDist, -PathLossExponent)
	signalPower := TxPowerWatts * signalGain

	interferencePower := 0.0

	if bs.State == STATE_COMMITTED {
		for neighborID, neighborColor := range bs.NeighborMap {
			if neighborColor == bs.CurrentPRB {
				// neighborID'den bana gelen ağırlık
				h_ji := bs.NeighborWeights[neighborID]
				interferencePower += (TxPowerWatts * h_ji)
			}
		}
	}

	sinr := signalPower / (interferencePower + NoiseWatts)

	capacity := BandwidthHz * math.Log2(1+sinr) / 1e6

	bs.Throughput = capacity

}
func main() {
	rand.Seed(time.Now().UnixNano())

	numDevice := 40    // İstasyon sayısı
	areaSize := 400.0  // Alan boyutu
	threshold := 100.0 // Komşuluk mesafesi

	fmt.Println("--- 5G Distributed Resource Management Simulation Commences ---")

	//İstasyonları Oluştur
	Network = make([]*BaseStation, numDevice)
	for i := 0; i < numDevice; i++ {
		Network[i] = NewBaseStation(Agent_ID(i), rand.Float64()*areaSize, rand.Float64()*areaSize)
	}

	// Topoloji ve Ağırlık Hesabı

	// Biz bu kodda rastgele sayı üretmiyoruz elektromanyetik dalgaların yayılım fiziğini simüle ediyoruz.
	// Doğada (ses, ışık ve radyo sinyalleri) bir kaynaktan uzaklaştıkça, sinyal gücü mesafenin karesiyle ters orantılı olarak azalır.
	// Buna fizikte Ters Kare Yasası denir

	// Mantık: Bir baz istasyonunun dibindeyken (mesafe az) sinyal çok güçlüdür ve "Girişim" (Interference) riski çok yüksektir
	// Mantık: İstasyondan çok uzaklaştığında (mesafe çok) sinyal zayıflar ve girişim etkisi neredeyse sıfıra iner.

	// 1. Temel Yol Kaybı (Inverse Square Law)  Deterministik
	// Şehir içi ortam varsayımıyla üssü 2.5 yapabiliriz

	// 2. Gölgeleme (Shadowing) - Stokastik / Rastgelelik
	// Binaların, ağaçların yarattığı rastgele sinyal değişimleri.
	// Standart Sapma (Sigma) = 0.5 (Orta yoğunluklu engel)

	fmt.Println("\n--- Calculating Path Loss & Shadowing ---")

	for i := 0; i < numDevice; i++ {
		for j := i + 1; j < numDevice; j++ {
			dist := Distance(Network[i], Network[j])

			// Eşik değer kontrolü (Menzil dışındaysa hesaplama yapma)
			if dist < threshold {

				baseWeight := ReferenceLoss * math.Pow(dist, -PathLossExponent)

				shadowing := math.Exp(rand.NormFloat64() * 0.5)

				// 3. Nihai Gerçekçi Ağırlık
				finalWeight := baseWeight * shadowing

				Network[i].NeighborWeights[Network[j].ID] = finalWeight
				Network[j].NeighborWeights[Network[i].ID] = finalWeight

				Network[i].Neighbros = append(Network[i].Neighbros, Network[j].ID)
				Network[j].Neighbros = append(Network[j].Neighbros, Network[i].ID)

				Network[i].Outbox[Network[j].ID] = Network[j].Inbox
				Network[j].Outbox[Network[i].ID] = Network[i].Inbox

				fmt.Printf("Link: BS-%d <--> BS-%d | Dist: %.1fm | Shadowing: %.2fx | Final Weight: %.5f\n",
					i, j, dist, shadowing, finalWeight)
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

	// ----------------------------------------------------

	fmt.Println("\n--- CALCULATING NETWORK THROUGHPUT (SHANNON CAPACITY) ---")

	totalNetworkCapacity := 0.0

	for _, bs := range Network {
		bs.CalculateShannonCapacity(Network) // Hesapla ve Struct'a kaydet
		totalNetworkCapacity += bs.Throughput

		fmt.Printf("BS-%d | Color: %d | Throughput: %.2f Mbps\n", bs.ID, bs.CurrentPRB, bs.Throughput)
	}

	fairnessScore := CalculateJainsFairness(Network)
	globalObjective := CalculateGlobalObjective(Network)

	fmt.Printf("\n>>> SYSTEM PERFORMANCE RESULTS V1 <<<\n")
	fmt.Printf("1. Total Network Capacity : %.2f Mbps (Higher is better)\n", totalNetworkCapacity)
	fmt.Printf("2. Average User Speed     : %.2f Mbps\n", totalNetworkCapacity/float64(numDevice))

	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("\n>>> SYSTEM PERFORMANCE V2 <<<\n")
	fmt.Printf("Jain's Fairness Index: %.4f (1.0 = Perfect Fairness)\n", fairnessScore)
	fmt.Printf("Global Objective (Total Interference): %.6f (Goal: Close to 0)\n", globalObjective)

	centralizedOptimal := CalculateCentralizedOptimum(Network)
	var poa float64
	epsilon := 1e-9

	if globalObjective < epsilon && centralizedOptimal < epsilon {
		poa = 1.0
	} else if centralizedOptimal < epsilon {
		poa = 999.0
	} else {
		poa = globalObjective / centralizedOptimal
	}

	fmt.Printf("Centralized Optimum (Benchmark)   : %.6f\n", centralizedOptimal)
	fmt.Printf(">>> PRICE OF ANARCHY (PoA)        : %.4f (Goal: Close to 1.0)\n", poa)
	fmt.Println("------------------------------------------------------------------")

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
		// Düğümleri ekle
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
			if bs.ID < neighborID { // Her kenarı bir kez eklemek için
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

	file, _ := json.MarshalIndent(data, "", " ")
	_ = os.WriteFile("viz_data.json", file, 0644)
	fmt.Println(" The data has been saved to the “viz_data.json” file.")
}
