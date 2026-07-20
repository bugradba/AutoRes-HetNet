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
		InterfShadow:    make(map[Agent_ID]float64), // Y-2: donmuş girişim gölgelemeleri
	}
}

// Start: İstasyonun ana yaşam döngüsünü başlatır.
// wg parametre olarak alınır ki her Monte Carlo koşusu kendi
// WaitGroup'unu kullanabilsin (global durum koşular arasında sızmasın).
func (bs *BaseStation) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	// Rastgele başlangıç gecikmesi (Herkes aynı anda başlamasın)
	time.Sleep(time.Duration(rand.Int63n(int64(StartDelayMax))))

	if Verbose {
		fmt.Printf("[BS-%d] Start (Position: %.1f, %.1f) -> Mod: Sensing\n", bs.ID, bs.X, bs.Y)
	}

	// Ticker: Her 500ms de bir "Düşün" (Think)
	ticker := time.NewTicker(ThinkPeriod)
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
		// CONFLICT sonrası backoff penceresi: süre dolmadan teklif verme.
		if time.Now().Before(bs.BackoffUntil) {
			return
		}
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

		// ESKİ log: "NASH EQUILIBRIUM REACHED" — tek istasyonun commit etmesi
		// denge kanıtı değildir; iddia H-2 düzeltmesiyle deney katmanındaki
		// VerifyNashEquilibrium denetimine taşındı.
		go func(proposed PRB) {
			time.Sleep(CommitTimeout)
			bs.Mutex.Lock()
			defer bs.Mutex.Unlock()
			// Eğer hala aynı teklifteysek ve girişim (Conflict) mesajı gelmediyse kazanmışızdır
			if bs.State == STATE_WAITING && bs.ProposedPRB == proposed {
				bs.State = STATE_COMMITTED
				bs.CurrentPRB = proposed
				if Verbose {
					fmt.Printf(" [BS-%d] COMMITTED -> Color %d\n", bs.ID, bs.CurrentPRB)
				}
				bs.Broadcast(MSG_SUCCESS, "Commit", bs.CurrentPRB)
			}
		}(bestPick)

	case STATE_COMMITTED:
		// ============================================================
		// H-2 DÜZELTMESİ: COMMITTED artık uç (terminal) durum değil.
		//
		// Eski protokolde istasyon, komşularının commit ANINDAKİ —
		// çoğu henüz kesinleşmemiş — bilgisine göre kilitleniyor ve
		// komşular sonradan farklı renklere geçtiğinde kararını asla
		// gözden geçirmiyordu. Sonuç: nihai tahsis çoğu koşuda Nash
		// dengesi değildi (sapma isteği var ama sapma mekanizması yok).
		//
		// Yeni davranış: her tick'te güncel NeighborMap'e göre
		// en-iyi-yanıt yeniden hesaplanır. Mevcut renkten KESİN olarak
		// daha az girişimli bir renk varsa istasyon PROPOSING'e döner
		// ve normal teklif/çekişme protokolüyle yeni rengi almaya
		// çalışır. Ağırlıklar simetrik olduğu için bu oyun bir "exact
		// potential game"dir; asenkron daha-iyi-yanıt adımları toplam
		// girişimi tekdüze azaltır ve dinamik sonlu adımda durur.
		//
		// %50 olasılık kapısı: iki komşunun aynı tick'te eşzamanlı
		// sapıp salınıma girmesini (simetriyi) kırmak için.
		bestPick := bs.CalculateBestResponse()
		if bestPick != bs.CurrentPRB &&
			bs.InterferenceFor(bestPick) < bs.InterferenceFor(bs.CurrentPRB)*(1-1e-9) &&
			rand.Intn(2) == 0 {
			if Verbose {
				fmt.Printf(" [BS-%d] Better response found (%d -> %d), re-proposing...\n",
					bs.ID, bs.CurrentPRB, bestPick)
			}
			bs.State = STATE_PROPOSING
			bs.ProposedPRB = -1
			// CurrentPRB korunur: yeni renk COMMIT edilene dek istasyon
			// fiilen eski rengi kullanmaya devam ediyor; komşular da
			// SUCCESS gelene kadar bizi eski rengimizle görmeli.
		}
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

// InterferenceFor: bs 'c' rengini kullansaydı, güncel yerel bilgisine
// (NeighborMap) göre katlanacağı toplam girişim ağırlığı.
// Hem en-iyi-yanıt hesabında hem H-2'nin COMMITTED denetiminde kullanılır.
func (bs *BaseStation) InterferenceFor(c PRB) float64 {
	total := 0.0
	for neighborID, neighborColor := range bs.NeighborMap {
		if neighborColor == c {
			total += bs.NeighborWeights[neighborID]
		}
	}
	return total
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
			// Teklifi haritaya İŞLEDİKTEN sonra itiraz et; eskiden buradaki
			// erken return komşu haritasını eksik bırakıyor, sonraki
			// en-iyi-yanıt hesapları eski bilgiyle yapılıyordu.
			bs.NeighborMap[msg.Sender_ID] = msg.Value
			bs.Send(msg.Sender_ID, MSG_CONFLICT, "That colourful!", bs.CurrentPRB)
			return
		}
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			if bs.ID > msg.Sender_ID {
				if Verbose {
					fmt.Printf("⚔ [BS-%d] Conflict! BS-%d'is rejecting (ID Priority).\n", bs.ID, msg.Sender_ID)
				}
				// H-1 DÜZELTMESİ: Eskiden buraya bs.CurrentPRB yazılıyordu.
				// WAITING durumundaki istasyonun CurrentPRB'si henüz -1 olduğu
				// için alıcıdaki "ProposedPRB == msg.Value" karşılaştırması hiç
				// tutmuyor ve itiraz sessizce yok sayılıyordu (ölü kod).
				// Doğrusu: itiraz edilen rengin KENDİSİNİ (msg.Value) göndermek.
				bs.Send(msg.Sender_ID, MSG_CONFLICT, "Wait your turn", msg.Value)
			}
		}
		bs.NeighborMap[msg.Sender_ID] = msg.Value
	case MSG_CONFLICT:
		//Itiraz etti.
		if bs.State == STATE_WAITING && bs.ProposedPRB == msg.Value {
			contested := msg.Value

			// İtiraz, o rengin komşuda kullanımda/istekte olduğunun
			// kanıtıdır; yerel haritayı güncelle ki sonraki en-iyi-yanıt
			// hesapları bu bilgiyi içersin.
			bs.NeighborMap[msg.Sender_ID] = contested

			// H-2 destek düzeltmesi (livelock önleme): Ağırlıklı oyunda
			// girişim yasak değil, MALİYETLİDİR. İtiraz eden komşunun
			// ağırlığı dahil edildiğinde bile bu renk hâlâ TÜM
			// alternatiflerden kesin olarak daha ucuzsa, geri çekilmek
			// yalnızca aynı teklifin sonsuza dek yinelenmesine yol açar.
			// Bu durumda teklifte kal (WAITING) ve zaman aşımında commit et.
			bestAltCost := math.MaxFloat64
			for c := PRB(0); c < MaxColors; c++ {
				if c == contested {
					continue
				}
				if cost := bs.InterferenceFor(c); cost < bestAltCost {
					bestAltCost = cost
				}
			}
			if bs.InterferenceFor(contested) < bestAltCost*(1-1e-9) {
				if Verbose {
					fmt.Printf(" [BS-%d] Objection noted but color %d is still my best response; staying.\n", bs.ID, contested)
				}
				return
			}

			if Verbose {
				fmt.Printf(" [BS-%d] Objection upheld! Withdrawn....\n", bs.ID)
			}
			// Y-3 DÜZELTMESİ: Eskiden burada mutex TUTULURKEN time.Sleep
			// yapılıyordu; ajan backoff boyunca hiçbir mesaj işleyemiyor,
			// ticker'ı ve commit zamanlayıcısı kilitte bekliyordu.
			// Doğrusu: kilidi tutmadan, bir sonraki teklif zamanını ileri
			// atmak. Think() SENSING dalı BackoffUntil'e saygı gösterir.
			bs.State = STATE_SENSING
			bs.ProposedPRB = -1
			bs.BackoffUntil = time.Now().Add(time.Duration(rand.Int63n(int64(BackoffMax))))
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
		// Non-blocking send: Kanal doluysa bekleme yapma, simülasyonu tıkama.
		// Y-4 DÜZELTMESİ: düşen mesajlar artık SESSİZCE kaybolmuyor —
		// sayaçlarla ölçülüyor. Düşen bir CONFLICT/SUCCESS yanlış commit'e
		// yol açabilir; sıklığı ölçülemeyen olay analiz edilemez.
		select {
		case ch <- Message{Sender_ID: bs.ID, Type: msgType, Payload: payload, Value: val}:
			bs.MsgSent.Add(1)
			if msgType == MSG_CONFLICT {
				bs.ConflictsSent.Add(1)
			}
		default:
			bs.MsgDropped.Add(1)
		}
	}
}
