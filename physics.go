package main

import "math"

// ============================================================
// G2: FİZİK KATMANI — 3GPP TR 38.901 URBAN MACRO (UMa)
//
// Bu dosya, tek üslü log-mesafe "oyuncak" modelin yerine 3GPP
// TR 38.901 (0.5-100 GHz) UMa senaryosunu uygular. Yayın düzeyinde
// kıyaslanabilirlik standart bir kanal modeli gerektirir.
//
// Uygulanan bileşenler (kaynak: TR 38.901 v17, Tablo 7.4.1-1 yol
// kaybı, Tablo 7.4.2-1 LOS olasılığı):
//
//   1. LOS/NLOS ayrımı, mesafeye bağlı LOS olasılığıyla çekilir.
//   2. UMa LOS: kırılma noktası (breakpoint) öncesi/sonrası iki
//      parçalı model; UMa NLOS: max(PL_LOS, PL'_NLOS) — standardın
//      gerektirdiği alt sınır uygulanır (NLOS, LOS'tan iyi olamaz).
//   3. Gölgeleme: LOS 4 dB, NLOS 6 dB (log-normal, dB'de Gauss).
//   4. Gürültü: N = -174 + 10log10(B) + NF dBm (NF = 7 dB).
//   5. Tavanlar: SINR <= 30 dB, spektral verim <= 7.4 bps/Hz
//      (5G NR 256-QAM pratik sınırı).
//
// TEKRARLANABİLİRLİK: Bu dosyada HİÇ rastgelelik yoktur. Tüm
// çekilişler (UE konumu, LOS durumları, gölgelemeler) koşu başında
// deney rng'sinden üretilip BaseStation üzerinde dondurulur
// (bkz. FreezeChannel). Aynı seed => aynı kanal => aynı throughput.
//
// GEOMETRİ: Girişim, BS<->BS kenar ağırlığından DEĞİL, girişimci
// istasyonun konumundan servis edilen KULLANICININ konumuna olan
// gerçek mesafeden hesaplanır. BS<->BS ağırlıkları yalnızca oyunun
// grafını kurar ve SINR hesabına girmez.
// ============================================================

// --- 38.901 YOL KAYBI ÇEKİRDEĞİ ---

// umaBreakpointDistance: TR 38.901'deki d'BP kırılma noktası.
// d'BP = 4 · h'BS · h'UT · fc / c, etkin yükseklikler h' = h - hE,
// UMa için hE = 1.0 m alınır. A3: txHeightM parametre (makro/piko).
func umaBreakpointDistance(txHeightM float64) float64 {
	const hE = 1.0
	const c = 3.0e8
	hBS := txHeightM - hE
	hUT := UEHeightM - hE
	return 4 * hBS * hUT * (CarrierFreqGHz * 1e9) / c
}

// PathLossUMaLOS: UMa LOS yol kaybı (dB). d2D yatay, d3D 3 boyutlu
// mesafe (m), fc GHz cinsindendir. A3: txHeightM verici yüksekliği.
//
//	d2D <= d'BP : PL1 = 28.0 + 22·log10(d3D) + 20·log10(fc)
//	d2D >  d'BP : PL2 = 28.0 + 40·log10(d3D) + 20·log10(fc)
//	                    − 9·log10(d'BP² + (hBS − hUT)²)
func PathLossUMaLOS(d2D, d3D, txHeightM float64) float64 {
	logFc := 20 * math.Log10(CarrierFreqGHz)
	dBP := umaBreakpointDistance(txHeightM)
	if d2D <= dBP {
		return 28.0 + 22*math.Log10(d3D) + logFc
	}
	dh := txHeightM - UEHeightM
	return 28.0 + 40*math.Log10(d3D) + logFc - 9*math.Log10(dBP*dBP+dh*dh)
}

// PathLossUMaNLOS: UMa NLOS yol kaybı (dB).
//
//	PL'_NLOS = 13.54 + 39.08·log10(d3D) + 20·log10(fc) − 0.6·(hUT − 1.5)
//	PL_NLOS  = max(PL_LOS, PL'_NLOS)
//
// max(...) standardın parçasıdır: NLOS bir bağlantı, aynı geometrideki
// LOS bağlantıdan daha az kayıplı olamaz.
func PathLossUMaNLOS(d2D, d3D, txHeightM float64) float64 {
	plNLOS := 13.54 + 39.08*math.Log10(d3D) + 20*math.Log10(CarrierFreqGHz) - 0.6*(UEHeightM-1.5)
	return math.Max(PathLossUMaLOS(d2D, d3D, txHeightM), plNLOS)
}

// LOSProbabilityUMa: TR 38.901 Tablo 7.4.2-1, UMa.
//
//	d2D <= 18 m : 1
//	aksi hâlde  : [18/d2D + e^(−d2D/63)·(1 − 18/d2D)]
//	              · [1 + C'(hUT)·(5/4)·(d2D/100)³·e^(−d2D/150)]
//	C'(hUT) = 0                      , hUT <= 13 m
//	          ((hUT − 13)/10)^1.5    , 13 < hUT <= 23 m
func LOSProbabilityUMa(d2D float64) float64 {
	if d2D <= 18.0 {
		return 1.0
	}
	cPrime := 0.0
	if UEHeightM > 13.0 {
		cPrime = math.Pow((UEHeightM-13.0)/10.0, 1.5)
	}
	base := 18.0/d2D + math.Exp(-d2D/63.0)*(1.0-18.0/d2D)
	corr := 1.0 + cPrime*1.25*math.Pow(d2D/100.0, 3)*math.Exp(-d2D/150.0)
	return base * corr
}

// PathLossUMa: yatay mesafe, LOS durumu ve verici yüksekliğinden yol
// kaybı (dB). A3: yükseklik artık parametre (makro 25 m, piko 10 m).
func PathLossUMa(d2D float64, los bool, txHeightM float64) float64 {
	d2D = math.Max(d2D, 10.0)
	dh := txHeightM - UEHeightM
	d3D := math.Sqrt(d2D*d2D + dh*dh)
	if los {
		return PathLossUMaLOS(d2D, d3D, txHeightM)
	}
	return PathLossUMaNLOS(d2D, d3D, txHeightM)
}

// LinkGain: yol kaybı + gölgelemeden doğrusal kanal kazancı.
// A3: verici yüksekliği parametre.
func LinkGain(d2D float64, los bool, shadowDB, txHeightM float64) float64 {
	return math.Pow(10, -(PathLossUMa(d2D, los, txHeightM)+shadowDB)/10.0)
}

// NoisePowerWatts: termal gürültü + alıcı gürültü şekli.
// N[dBm] = −174 + 10·log10(B[Hz]) + NF
func NoisePowerWatts() float64 {
	dBm := -174.0 + 10*math.Log10(BandwidthHz) + NoiseFigureDB
	return math.Pow(10, dBm/10.0) / 1000.0 // dBm -> W
}

// ShadowSigmaDB: bağlantı durumuna göre gölgeleme std sapması.
func ShadowSigmaDB(los bool) float64 {
	if los {
		return ShadowSigmaLOSdB
	}
	return ShadowSigmaNLOSdB
}

// --- SINR VE KAPASİTE ---

// dist2D: iki nokta arası yatay mesafe.
func dist2D(x1, y1, x2, y2 float64) float64 {
	dx, dy := x1-x2, y1-y2
	return math.Sqrt(dx*dx + dy*dy)
}

// interferencePowerAt: verilen renk haritasına göre, bs'nin
// KULLANICISININ konumunda ölçülen toplam girişim gücü (Watt).
// Yalnızca gerçekten ileten (renk atanmış) eş-kanal komşular sayılır;
// her katkı girişimci-BS -> kullanıcı geometrisiyle hesaplanır.
// interferencePowerAt: verilen renk haritasına göre, bs'nin
// KULLANICISININ konumunda ölçülen toplam girişim gücü (Watt).
//
// A2 DÜZELTMESİ: Girişim, oyunun komşuluk grafıyla (Neighbros) DEĞİL,
// bu kullanıcıya girişim yapabilecek TÜM eş-kanal istasyonlar
// (Interferers, girişim yarıçapı içinde) üzerinden toplanır. Eş-kanal
// bir istasyon 100 m oyun eşiğinin dışında olsa bile fiziksel girişim
// yapar; eski kod bunları sıfır sayıyordu.
func interferencePowerAt(bs *BaseStation, myColor PRB, byID map[Agent_ID]*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	interference := 0.0
	for _, interfererID := range bs.Interferers {
		nc, ok := colorOf[interfererID]
		if !ok || nc == -1 || nc != myColor {
			continue // iletmeyen ya da farklı kanaldaki istasyon girişim yapmaz
		}
		interferer, ok := byID[interfererID]
		if !ok {
			continue
		}
		d := dist2D(interferer.X, interferer.Y, bs.UserX, bs.UserY)
		gain := LinkGain(d, bs.InterfLOS[interfererID], bs.InterfShadowDB[interfererID], interferer.HeightM)
		interference += interferer.TxWatts * gain // A3: girişimcinin kendi gücü
	}
	return interference
}

// SINRForColor: bs 'color' rengiyle iletseydi kullanıcısının göreceği
// doğrusal SINR (tavan uygulanmış). color == -1 için 0 döner.
func SINRForColor(bs *BaseStation, color PRB, byID map[Agent_ID]*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	if color == -1 {
		return 0.0
	}
	dServ := dist2D(bs.X, bs.Y, bs.UserX, bs.UserY)
	signal := bs.TxWatts * LinkGain(dServ, bs.ServingLOS, bs.ServingShadowDB, bs.HeightM) // A3: kendi gücü/yüksekliği

	interference := interferencePowerAt(bs, color, byID, colorOf)

	sinr := signal / (interference + NoisePowerWatts())
	return math.Min(sinr, math.Pow(10, SINRCapDB/10.0)) // <= 30 dB
}

// CapacityForColor: bs 'color' rengiyle iletseydi, donmuş kanal
// gerçekleşmesi altında kullanıcısının elde edeceği hız (Mbps).
// Renk atanmamış (-1) istasyon iletemez => 0 Mbps.
func CapacityForColor(bs *BaseStation, color PRB, network []*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	if color == -1 {
		return 0.0
	}
	return capacityWithIndex(bs, color, indexByID(network), colorOf)
}

func capacityWithIndex(bs *BaseStation, color PRB, byID map[Agent_ID]*BaseStation, colorOf map[Agent_ID]PRB) float64 {
	if color == -1 {
		return 0.0
	}
	sinr := SINRForColor(bs, color, byID, colorOf)
	se := math.Min(math.Log2(1+sinr), SpectralEffCapBpsHz) // <= 7.4 bps/Hz
	return BandwidthHz * se / 1e6                          // Mbps
}

// indexByID: Agent_ID -> istasyon araması (O(N) kurulum, O(1) erişim).
func indexByID(network []*BaseStation) map[Agent_ID]*BaseStation {
	byID := make(map[Agent_ID]*BaseStation, len(network))
	for _, node := range network {
		byID[node.ID] = node
	}
	return byID
}

// CalculateShannonCapacity: istasyonun MEVCUT rengine göre hızını
// hesaplayıp alanına yazar. Girişim, ağın yer gerçeği renklerinden
// okunur (ajanın olası eskimiş NeighborMap'inden değil).
func (bs *BaseStation) CalculateShannonCapacity(network []*BaseStation) {
	bs.Throughput = CapacityForColor(bs, bs.CurrentPRB, network, ColorsOfNetwork(network))
}

// ThroughputsForAssignment: verilen tahsis için tüm istasyonların
// hızları — AYNI donmuş kanal üzerinde. Baseline şemalarının dağıtık
// çözümle adil karşılaştırılmasını sağlar: fark yalnızca tahsistendir.
func ThroughputsForAssignment(network []*BaseStation, colorOf map[Agent_ID]PRB) []float64 {
	byID := indexByID(network)
	caps := make([]float64, len(network))
	for i, bs := range network {
		caps[i] = capacityWithIndex(bs, colorOf[bs.ID], byID, colorOf)
	}
	return caps
}
