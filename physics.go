package main

import "math"

// ============================================================
// Y-2 DÜZELTMESİ: FİZİK KATMANI YENİDEN YAZIMI
//
// Eski modelin sorunları ve burada nasıl giderildikleri:
//
//  1. TOHUM İHLALİ: Eski kod her çağrıda time.Now().UnixNano() ile
//     kendi RNG'sini kuruyordu; aynı seed iki farklı throughput
//     üretiyordu. YENİ: tüm rastgelelik (kullanıcı konumu + tüm
//     gölgeleme çekilişleri) koşu başında deney RNG'sinden BİR KEZ
//     çizilip BaseStation üzerinde dondurulur (FreezeChannel).
//     Bu dosyada hiç RNG yoktur; fonksiyonlar tamamen deterministiktir.
//
//  2. "CELL SHRINKAGE": Kullanıcı mesafesi istasyonun girişim
//     durumuna göre seçiliyordu (girişim varsa yakın, yoksa uzak) —
//     karşılaştırılan şemalar arasında sistematik yanlılık. YENİ:
//     kullanıcı konumu tahsisten BAĞIMSIZ, koşu başında sabittir;
//     tüm şemalar aynı kullanıcıyı servis eder.
//
//  3. YANLIŞ GİRİŞİM GEOMETRİSİ: Girişim, girişimci-BS -> kullanıcı
//     kazancı yerine BS -> BS kenar ağırlığıyla hesaplanıyordu.
//     YENİ: girişim, girişimci istasyonun KONUMUNDAN kullanıcının
//     KONUMUNA gerçek mesafe üzerinden (path loss + donmuş
//     gölgeleme) hesaplanır.
//
//  4. TAVAN YOK: SINR ve spektral verimlilik sınırsızdı (~12.4 bps/Hz
//     gözlenmişti; 256-QAM pratiği ~7.4). YENİ: SINR <= 30 dB ve
//     spektral verimlilik <= 8 bps/Hz (20 MHz'te 160 Mbps) tavanları.
//
//  5. FAILED İSTASYON ŞİŞİRMESİ (eski 'Hata 4'): COMMIT edemeyen
//     istasyon girişimsiz sayılıp şişkin throughput ile ortalamaya
//     giriyordu. YENİ: renk atanmamış (PRB=-1) istasyon iletim
//     yapamaz; throughput = 0.
//
//  6. Girişim ajanın kendi (eksik olabilen) NeighborMap'inden değil,
//     ağın YER GERÇEĞİ renklerinden hesaplanır: bu bir ölçüm
//     (measurement) fonksiyonudur, ajan kararı değil.
// ============================================================

// interferencePowerAt: verilen renk haritasına göre, bs'nin
// kullanıcısının konumunda gördüğü toplam girişim gücü (Watt).
// Yalnızca gerçekten iletim yapan (renk atanmış) eş-kanal komşular
// sayılır; geometri girişimci-BS -> kullanıcı mesafesidir.
func interferencePowerAt(bs *BaseStation, myColor PRB, network []*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	byID := make(map[Agent_ID]*BaseStation, len(network))
	for _, node := range network {
		byID[node.ID] = node
	}

	interference := 0.0
	for _, neighborID := range bs.Neighbros {
		nc, ok := colorOf[neighborID]
		if !ok || nc == -1 || nc != myColor {
			continue // iletmeyen ya da farklı kanaldaki komşu girişim yapmaz
		}
		interferer := byID[neighborID]

		// GERÇEK girişimci -> kullanıcı geometrisi:
		dx := interferer.X - bs.UserX
		dy := interferer.Y - bs.UserY
		dist := math.Max(1.0, math.Sqrt(dx*dx+dy*dy)) // referans mesafesi altına inme

		gain := ReferenceLoss * math.Pow(dist, -PathLossExponent) * bs.InterfShadow[neighborID]
		interference += TxPowerWatts * gain
	}
	return interference
}

// CapacityForColor: bs 'color' rengiyle iletim yapsaydı, donmuş kanal
// gerçekleşmesi altında kullanıcısının göreceği kapasite (Mbps).
// color == -1 (iletim yok) için 0 döner.
func CapacityForColor(bs *BaseStation, color PRB, network []*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	if color == -1 {
		return 0.0 // Hata 4 düzeltmesi: iletmeyen istasyonun hızı yoktur
	}

	// Serving link: BS -> kendi kullanıcısı (donmuş konum + gölgeleme)
	dx := bs.X - bs.UserX
	dy := bs.Y - bs.UserY
	servingDist := math.Max(1.0, math.Sqrt(dx*dx+dy*dy))
	signalGain := ReferenceLoss * math.Pow(servingDist, -PathLossExponent) * bs.ServingShadow
	signalPower := TxPowerWatts * signalGain

	interferencePower := interferencePowerAt(bs, color, network, colorOf)

	// SINR + tavanlar
	sinr := signalPower / (interferencePower + NoiseWatts)
	sinr = math.Min(sinr, SINRCapLinear) // <= 30 dB

	spectralEff := math.Min(math.Log2(1+sinr), SpectralEffCapBpsHz) // <= 8 bps/Hz
	return BandwidthHz * spectralEff / 1e6                          // Mbps (20 MHz'te <= 160)
}

// CalculateShannonCapacity: istasyonun MEVCUT (dağıtık protokolün
// ürettiği) rengine göre throughput'unu hesaplayıp Struct'a yazar.
// Girişim, ağın yer gerçeği renklerinden okunur.
func (bs *BaseStation) CalculateShannonCapacity(network []*BaseStation) {
	colorOf := ColorsOfNetwork(network)
	bs.Throughput = CapacityForColor(bs, bs.CurrentPRB, network, colorOf)
}

// ThroughputsForAssignment: verilen tahsis (renk haritası) için tüm
// istasyonların kapasitelerini döndürür — AYNI donmuş kanal üzerinde.
// Baseline şemalarının (greedy/DSATUR/fixed/random) dağıtık çözümle
// adil karşılaştırılmasını sağlar: fark yalnızca tahsise atfedilebilir.
func ThroughputsForAssignment(network []*BaseStation, colorOf map[Agent_ID]PRB) []float64 {
	caps := make([]float64, len(network))
	for i, bs := range network {
		caps[i] = CapacityForColor(bs, colorOf[bs.ID], network, colorOf)
	}
	return caps
}
