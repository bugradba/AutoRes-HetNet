package main

import (
	"sync"
	"sync/atomic"
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

	// Y-2 DÜZELTMESİ: gerçekçi PHY tavanları (README'nin vaadi).
	SINRCapLinear       = 1000.0 // SINR <= 30 dB
	SpectralEffCapBpsHz = 8.0    // <= 8 bps/Hz (20 MHz'te 160 Mbps tavanı)

	// Donmuş kullanıcı yerleşimi: koşu başında tohumlu rng ile çizilir,
	// istasyonun girişim durumuna GÖRE DEĞİL (eski "cell shrinkage"
	// mekanizması metodolojik olarak savunulamazdı ve kaldırıldı).
	UserMinDist = 10.0
	UserMaxDist = 150.0

	// Gölgeleme çarpanı: exp(sigma_ln * N(0,1)). Graf ağırlıklarıyla
	// aynı model; hem serving hem girişim linklerine simetrik uygulanır.
	ShadowSigmaLn = 0.5
)

// --- ALGORİTMA SABİTLERİ ---
// Y-1: MaxColors artık değişken; -sweep modu K'yi hücre başına değiştirir.
// Sweep dışında program boyunca sabittir (tüm dosyalarda tek kaynak).
var MaxColors PRB = 5

// --- PROTOKOL ZAMANLAYICILARI (ölçeklenebilir) ---
// Y-1 (-timescale): tüm süreler tek çarpanla ölçeklenir, ORANLAR korunur.
// Yakınsama "protokol turu" (think-period) cinsinden raporlandığı için
// sonuçlar zaman ölçeğinden bağımsızdır; 0.1 => 10x hızlı deney.
var (
	ThinkPeriod   = 500 * time.Millisecond // ajanın karar periyodu (1 protokol turu)
	CommitTimeout = 2 * time.Second        // CONFLICT beklenen pencere
	BackoffMax    = 500 * time.Millisecond // CONFLICT sonrası rastgele geri çekilme üst sınırı
	StartDelayMax = 1 * time.Second        // rastgele başlangıç gecikmesi üst sınırı
	QuietWindow   = 3 * time.Second        // H-2: sessizlik penceresi (yakınsama tanımı)
	PollInterval  = 50 * time.Millisecond  // deney katmanı yoklama aralığı
	MaxWait       = 20 * time.Second       // livelock'a karşı güvenlik üst sınırı
)

// SetTimescale: tüm protokol zamanlayıcılarını s çarpanıyla ölçekler.
// main() içinde, herhangi bir simülasyondan ÖNCE bir kez çağrılmalıdır.
func SetTimescale(s float64) {
	scale := func(d time.Duration) time.Duration {
		return time.Duration(float64(d) * s)
	}
	ThinkPeriod = scale(500 * time.Millisecond)
	CommitTimeout = scale(2 * time.Second)
	BackoffMax = scale(500 * time.Millisecond)
	StartDelayMax = scale(1 * time.Second)
	QuietWindow = scale(3 * time.Second)
	PollInterval = scale(50 * time.Millisecond)
	MaxWait = scale(20 * time.Second)
}

// --- SİMÜLASYON PARAMETRELERİ ---
// Hata 2 düzeltmesi: makaledeki HER sayı bu sabitlerle üretilmeli.
// Üç ayrı kaynakta (makale/README/kod) üç farklı değer olmamalı.
const (
	SimN         = 40    // İstasyon sayısı
	SimAreaSize  = 400.0 // Alan boyutu (metre, kare kenarı)
	SimThreshold = 100.0 // Komşuluk / girişim eşiği (metre)
)

// Verbose: ajanların adım adım log basıp basmayacağı.
// Tek koşuda eğitici; Monte Carlo'da (yüzlerce koşu) kapatılır,
// aksi halde çıktı okunamaz hale gelir ve koşu yavaşlar.
var Verbose = true

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
	Quit            chan struct{}    // Stop() ile kapatılır; Start döngüsünü sonlandırır
	State           AgentState       //Şu an Ne yapıyor
	CurrentPRB      PRB              //Kazandığı Renk
	ProposedPRB     PRB              //İstegiği renk
	NeighborMap     map[Agent_ID]PRB // Komşuların renklerini tuttuğu hafıza
	Throughput      float64          // Mbps cinsinden veri hızı
	BackoffUntil    time.Time        // CONFLICT sonrası bu ana kadar yeni teklif verme (kilitli uyku yerine)

	// --- Y-2: DONMUŞ KANAL GERÇEKLEŞMESİ (frozen channel) ---
	// Koşu başında deney rng'sinden BİR KEZ çizilir ve koşu boyunca
	// sabittir. Böylece (i) aynı seed aynı throughput'u üretir,
	// (ii) tüm tahsis şemaları AYNI kanal üzerinde karşılaştırılır
	// (fark yalnızca tahsise atfedilebilir).
	UserX, UserY  float64              // servis edilen kullanıcının konumu
	ServingShadow float64              // serving link gölgeleme çarpanı
	InterfShadow  map[Agent_ID]float64 // girişimci-BS -> BU kullanıcı gölgelemeleri

	// --- Y-4: MESAJ SAYAÇLARI ---
	// Send, ana goroutine dışındaki commit-zamanlayıcı goroutine'lerinden
	// de çağrıldığı için atomik; deney katmanı koşu sonunda okur.
	MsgSent       atomic.Int64 // başarıyla kuyruğa giren mesaj
	MsgDropped    atomic.Int64 // kanal dolu olduğu için DÜŞEN mesaj
	ConflictsSent atomic.Int64 // gönderilen CONFLICT sayısı

	Mutex sync.Mutex
}

// NOT: Eski koddaki global Network ve wg değişkenleri kaldırıldı.
// Monte Carlo'da her koşu kendi ağını ve kendi WaitGroup'unu kurar;
// global durum, koşular arasında sızıntıya ve veri yarışına yol açar.
