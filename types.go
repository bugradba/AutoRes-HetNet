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

// --- FİZİK KATMANI: 3GPP TR 38.901 UMa (G2) ---
//
// Kanal modeli, tek üslü log-mesafe yaklaşımından 3GPP TR 38.901
// (0.5-100 GHz) Urban Macro senaryosuna yükseltildi. Yayınlarda
// kıyaslanabilirliğin ön koşulu standart bir kanal modelidir.
const (
	// Taşıyıcı ve anten geometrisi (TR 38.901 Tablo 7.2-1, UMa)
	CarrierFreqGHz = 3.5  // fc: 5G NR n78 bandı
	BSHeightM      = 25.0 // hBS: UMa makro anten yüksekliği (piko için PicoHeightM)
	UEHeightM      = 1.5  // hUT: kullanıcı terminali yüksekliği

	// Verici ve alıcı zinciri
	TxPowerWatts  = 40.0 // 46 dBm (20 MHz makro) — varsayılan/geriye uyumluluk
	BandwidthHz   = 20e6 // B: 20 MHz
	NoiseFigureDB = 7.0  // NF: alıcı gürültü şekli (N = -174 + 10log10(B) + NF)

	// --- A3: HETEROJEN AĞ (HetNet) İSTASYON TİPLERİ ---
	// Proje adı "HetNet" gerçek anlamını burada kazanır: ağ artık homojen
	// makro istasyonlardan değil, makro + piko karışımından oluşur.
	// Değerler 3GPP HetNet değerlendirme varsayımlarına dayanır.
	MacroTxWatts = 40.0 // 46 dBm makro hücre gücü
	PicoTxWatts  = 1.0  // 30 dBm piko hücre gücü (makrodan ~16 dB düşük)
	MacroHeightM = 25.0 // makro anten yüksekliği
	PicoHeightM  = 10.0 // piko anten yüksekliği (daha alçak, sokak seviyesi)

	// Gölgeleme std sapmaları (TR 38.901 Tablo 7.4.1-1)
	ShadowSigmaLOSdB  = 4.0 // LOS bağlantılar
	ShadowSigmaNLOSdB = 6.0 // NLOS bağlantılar

	// Pratik alıcı tavanları
	SINRCapDB           = 30.0 // SINR <= 30 dB
	SpectralEffCapBpsHz = 7.4  // 5G NR 256-QAM pratik üst sınırı (20 MHz'te 148 Mbps)

	// Donmuş kullanıcı yerleşimi. Alt sınır 10 m, TR 38.901'in
	// geçerlilik aralığıdır (model d2D >= 10 m için tanımlıdır).
	UserMinDist = 10.0
	UserMaxDist = 150.0
)

// PicoFraction (A3): istasyonların piko olma oranı. -pico-fraction ile
// ayarlanır; 0 = homojen makro ağ (A3 öncesi davranış, ablasyon için).
var PicoFraction = 0.5

// --- A1: KUPLAJ MODU (oyunun maliyet fonksiyonu) ---
//
// physical  : w_ij = ½·Ptx·[G(j->UE_i) + G(i->UE_j)]  (varsayılan)
//
//	Fiziksel, simetrik, 38.901 kanalından türetilir.
//
// geometric : w_ij = C·d_ij^(-α)·gölgeleme  (eski/ablasyon)
//
//	BS<->BS mesafesine dayanan geometrik vekil.
type CouplingKind int

const (
	CouplingPhysical CouplingKind = iota
	CouplingGeometric
)

var CouplingMode = CouplingPhysical

func (c CouplingKind) String() string {
	if c == CouplingGeometric {
		return "geometric"
	}
	return "physical"
}

// --- OYUN GRAFI KUPLAJ SABİTLERİ (yalnızca geometric mod) ---
//
// DİKKAT: Aşağıdaki iki sabit YALNIZCA oyunun girişim grafını (BS<->BS
// kenar ağırlıkları) kurmak için kullanılır ve SINR hesabına GİRMEZ.
// Grafik ağırlığı, ajanların maliyet fonksiyonundaki soyut "kuplaj
// şiddeti"dir; fiziksel değerlendirme (throughput/SINR) tamamen
// 38.901 UMa ile ve gerçek girişimci->UE geometrisi üzerinden yapılır.
// İki katmanın ayrı tutulması bilinçli bir tasarım tercihidir:
// oyun grafı simetriktir (exact potential game koşulu), gerçek
// girişim kuplajı ise yönlüdür.
const (
	CouplingRefLoss  = 1e-4 // 1 m referans kaybı (-40 dB)
	CouplingExponent = 3.0  // graf kuplajı için sönümleme üssü
	CouplingShadowLn = 0.5  // graf kuplajı log-normal gölgeleme parametresi
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
//
// SimN artık değişken: -N bayrağıyla değiştirilebilir. Küçük N'de
// (ör. 10-16) B&B optimumu her koşuda kanıtlayabildiği için PoA
// güvenilir raporlanır; N=40 tipik olarak tek dev bileşen ürettiğinden
// (NP-zor, bileşen ayrıştırma fayda etmez) PoA yalnızca kanıtlanan
// koşularda ve seçim yanlılığı notuyla raporlanmalıdır.
var SimN = 40 // İstasyon sayısı (-N ile değiştirilebilir)

const (
	SimAreaSize  = 400.0 // Alan boyutu (metre, kare kenarı)
	SimThreshold = 100.0 // Komşuluk / OYUN grafı eşiği (metre) — kim kiminle mesajlaşır
)

// InterfRadius (A2): fiziksel girişim yarıçapı. Girişim, oyunun komşuluk
// eşiğiyle (SimThreshold) DEĞİL, bu yarıçapla sınırlanır. Girişim gücü
// mesafenin ~PathLossExponent kuvvetiyle düştüğünden, bu yarıçaptan uzak
// istasyonların katkısı ihmal edilebilir. Değer, tüm ağı kapsayacak
// kadar büyük (alan köşegeni ~566 m) seçilirse girişim EKSİKSİZ toplanır;
// pratikte 250 m yeterli doğruluğu çok daha az bellek/hesapla verir.
// SimThreshold'un (100 m) belirgin biçimde üstünde olması, A2'nin
// çözdüğü "eşik-dışı eş-kanal girişimci" sorununu kapsar.
var InterfRadius = 600.0 // varsayılan: tüm ağ (eksiksiz girişim)

// AblateIDPriority (deney anahtarı): true ise WAITING-WAITING
// çekişmelerinde ID-öncelik itirazı GÖNDERİLMEZ — H-1 hatasının fiilî
// etkisinin (itirazın ölü kod olması) kontrollü yeniden üretimi.
// Amaç: "çekişme çözümünün ampirik değeri"ni mevcut HEAD'den tek
// bayrakla, önce/sonra olarak raporlayabilmek (ablation study).
var AblateIDPriority = false

// AblateCommitRecheck (deney anahtarı): true ise COMMITTED durumdaki
// periyodik en-iyi-yanıt denetimi (H-2 düzeltmesi) kapatılır; COMMITTED
// yeniden uç durum olur. İki bayrak birlikte 2x2 ablasyon ızgarası kurar:
//
//	her ikisi açık  = mevcut protokol
//	her ikisi kapalı = orijinal (H-1'li) protokolün fiilî davranışı
var AblateCommitRecheck = false

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
	ID   Agent_ID
	X, Y float64 // Şimdilik konum ve mesafe hesabı için

	// --- A3: HetNet istasyon tipi ---
	// Her istasyonun kendi verici gücü ve anten yüksekliği vardır
	// (makro: 40 W/25 m, piko: 1 W/10 m). SINR hesabı global TxPowerWatts
	// yerine bu alanları kullanır; homojen ağ gerçek bir HetNet'e dönüşür.
	IsPico  bool
	TxWatts float64 // bu istasyonun verici gücü (W)
	HeightM float64 // bu istasyonun anten yüksekliği (m)

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

	// --- G2/A2: DONMUŞ KANAL GERÇEKLEŞMESİ (3GPP TR 38.901 UMa) ---
	// Koşu başında deney rng'sinden BİR KEZ çizilir ve koşu boyunca
	// sabittir. Böylece (i) aynı seed aynı throughput'u üretir,
	// (ii) tüm tahsis şemaları AYNI kanal üzerinde karşılaştırılır.
	//
	// A2 DÜZELTMESİ: Girişim artık komşuluk grafıyla (SimThreshold)
	// SINIRLI DEĞİL. Eş-kanal bir istasyon, oyunun 100 m eşiğinin
	// dışında olsa bile bu kullanıcıya fiziksel girişim yapar. Bu yüzden
	// LOS/gölgeleme, yalnızca Neighbros için değil, girişim yarıçapı
	// (InterfRadius) içindeki TÜM istasyonlar için dondurulur.
	// (Girişim gücü mesafenin ~3. kuvvetiyle düştüğü için pratikte çok
	// uzak istasyonların katkısı ihmal edilebilir; InterfRadius bu yüzden
	// bellek/doğruluk dengesi kuran bir sınırdır, oyun eşiğinden bağımsızdır.)
	UserX, UserY    float64              // servis edilen kullanıcının konumu
	ServingLOS      bool                 // serving link LOS mu?
	ServingShadowDB float64              // serving link gölgeleme (dB)
	InterfLOS       map[Agent_ID]bool    // girişimci-BS -> BU kullanıcı: LOS mu? (tüm girişimciler)
	InterfShadowDB  map[Agent_ID]float64 // girişimci-BS -> BU kullanıcı: gölgeleme (dB)
	Interferers     []Agent_ID           // bu kullanıcıya girişim yapabilecek TÜM istasyonlar (oyun komşularından geniş)

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
