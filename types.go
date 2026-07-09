package main

import (
	"sync"
	"time"
)

type Agent_ID int

type PRB int

type MessageType int

type AgentState int

const (
	MSG_HELLO MessageType = iota // iota: Go'da ardışık sabit üretici
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
	NoiseWatts       = 1e-13 // -100 dBm
	BandwidthHz      = 20e6  // 20 MHz
	ReferenceLoss    = 1e-4  // d0 = 1 m referans kaybı (-40 dB)
	PathLossExponent = 3.0   // şehir içi sönümleme katsayısı
	ShadowSigma      = 0.5   // log-normal gölgeleme sigma'sı (BuildNetwork ile AYNI model)

	// HATA 5 DÜZELTMESİ: fiziksel tavanlar. Tavansız Shannon formülü,
	// 10 m'deki kullanıcı için SINR ~4e7 ve 20 MHz'te ~500 Mbps gibi
	// fiziksel olmayan değerler üretir. Pratik LTE/NR MCS tavanları:
	MaxSINR        = 1e3 // ~30 dB SINR tavanı
	MaxSpectralEff = 8.0 // bps/Hz tavanı => 20 MHz'te en çok 160 Mbps

	// Kullanıcı (UE) yerleşimi: servis BS'ten uzaklık aralığı.
	UEMinDist = 10.0
	UEMaxDist = 100.0
)

// --- ALGORİTMA SABİTLERİ ---
// MaxColors artık var: -sweep modu K'yi ızgara üzerinde değiştirir.
var MaxColors = 5 // K: ortogonal renk / PRB sayısı (tüm dosyalarda tek kaynak)

// --- SİMÜLASYON PARAMETRELERİ ---
// Hata 2 düzeltmesi: makaledeki HER sayı bu sabitlerle üretilmeli.
const (
	SimN         = 40    // istasyon sayısı
	SimAreaSize  = 400.0 // alan boyutu (metre, kare kenarı)
	SimThreshold = 100.0 // komşuluk / girişim eşiği (metre)
)

// --- PROTOKOL ZAMANLAYICILARI ---
// Gözlem: yakınsama süresini algoritma değil bu SABİT zamanlayıcılar
// domine eder (başlangıç gecikmesi + think periyodu + commit zaman aşımı).
// Bu yüzden: (1) hepsi tek kaynaktan yönetilir ve SetTimeScale ile
// orantılı ölçeklenir (tarama deneylerini 10x+ hızlandırır),
// (2) yakınsama, ölçekten bağımsız "protokol turu" (ThinkPeriod)
// cinsinden raporlanır.
var (
	StartDelayMax = 1000 * time.Millisecond // rastgele başlangıç gecikmesi üst sınırı
	ThinkPeriod   = 500 * time.Millisecond  // Think() periyodu (1 protokol turu)
	CommitTimeout = 2000 * time.Millisecond // CONFLICT beklemeden commit süresi
	BackoffMax    = 500 * time.Millisecond  // çakışma sonrası rastgele geri çekilme üst sınırı
)

// SetTimeScale: tüm protokol zamanlayıcılarını orantılı ölçekler.
// Oranlar korunduğu için protokol dinamiği (mesaj yarışları dahil)
// niteliksel olarak aynı kalır; yalnızca duvar saati sıkışır.
func SetTimeScale(s float64) {
	scale := func(base time.Duration) time.Duration {
		d := time.Duration(float64(base) * s)
		if d < time.Millisecond {
			d = time.Millisecond // zamanlayıcı sıfıra inmesin
		}
		return d
	}
	StartDelayMax = scale(1000 * time.Millisecond)
	ThinkPeriod = scale(500 * time.Millisecond)
	CommitTimeout = scale(2000 * time.Millisecond)
	BackoffMax = scale(500 * time.Millisecond)
}

// Verbose: ajanların adım adım log basıp basmayacağı.
// Tek koşuda eğitici; Monte Carlo'da kapatılır.
var Verbose = true

type Message struct {
	Sender_ID Agent_ID
	Type      MessageType
	Payload   string
	Value     PRB
}

type BaseStation struct {
	ID              Agent_ID
	X, Y            float64              // konum
	NeighborWeights map[Agent_ID]float64 // ağırlıklı girişim grafı
	Inbox           chan Message
	Outbox          map[Agent_ID]chan Message
	Neighbros       []Agent_ID
	Quit            chan struct{}    // Stop() ile kapatılır; Start döngüsünü sonlandırır
	State           AgentState       // şu an ne yapıyor
	CurrentPRB      PRB              // commit edilen renk
	ProposedPRB     PRB              // önerilen renk
	NeighborMap     map[Agent_ID]PRB // komşuların son bilinen renkleri
	Throughput      float64          // Mbps
	Mutex           sync.Mutex

	// Protokol iç durumu:
	// backoffUntil: CONFLICT sonrası bu ana kadar yeni teklif verilmez.
	//   (DÜZELTME: eski kod backoff'u HandleMessage içinde, MUTEX KİLİTLİYKEN
	//   time.Sleep ile yapıyordu — bu, ajanın mesaj döngüsünü 0,5 s'ye kadar
	//   bloke ediyor ve commit zamanlayıcısını kilitte bekletiyordu.)
	// proposalSeq: her yeni teklifte artar; commit zamanlayıcısı yalnızca
	//   KENDİ teklifinin hâlâ geçerli olduğunu bu sayıyla doğrular.
	//   (DÜZELTME: eski kod yalnızca rengi karşılaştırıyordu; geri çekilip
	//   2 s içinde AYNI rengi yeniden öneren istasyonu BAYAT zamanlayıcı
	//   erken commit edebiliyordu.)
	backoffUntil time.Time
	proposalSeq  uint64

	// Mesaj enstrümantasyonu (yakınsama analizi için):
	// MsgSent[t]: t tipinde başarıyla kuyruğa giren mesaj sayısı;
	// MsgDropped: alıcı kuyruğu dolu olduğu için DÜŞEN mesajlar
	// (Send non-blocking). atomic ile artırılır: commit zamanlayıcısı
	// goroutine'i ile ana döngü aynı anda gönderebilir.
	MsgSent    [4]int64
	MsgDropped int64
}

// NOT: Eski koddaki global Network ve wg değişkenleri kaldırıldı.
// Monte Carlo'da her koşu kendi ağını ve kendi WaitGroup'unu kurar.
