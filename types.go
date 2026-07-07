package main

import "sync"

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

// --- FİZİKSEL KATMAN MODEL SABİTLERİ (Hata 5) ---
const (
	ShadowSigma    = 0.5   // log-normal gölgeleme σ'sı (doğal log ölçeğinde; topoloji ve PHY aynı modeli kullanır)
	UEMinDist      = 10.0  // kullanıcının servis BS'ine minimum uzaklığı (m)
	UEMaxDist      = 100.0 // kullanıcının servis BS'ine maksimum uzaklığı (m)
	MaxSINR        = 1e3   // ~30 dB SINR tavanı (pratik alıcı sınırı)
	MaxSpectralEff = 8.0   // bps/Hz (≈ 256-QAM pratik üst sınırı)
)

// --- ALGORİTMA SABİTLERİ ---
const (
	MaxColors = 5 // K: ortogonal renk / PRB sayısı (tüm dosyalarda tek kaynak)
)

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
	Mutex           sync.Mutex
}

// NOT: Eski koddaki global Network ve wg değişkenleri kaldırıldı.
// Monte Carlo'da her koşu kendi ağını ve kendi WaitGroup'unu kurar;
// global durum, koşular arasında sızıntıya ve veri yarışına yol açar.
