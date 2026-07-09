package main

import (
	"math"
	"math/rand"
)

// SINR ve SHANNON KAPASİTE HESABI (Hata 4, 5 ve 6 düzeltilmiş model)
//
// İndirme yönü (downlink) SINR'ı KULLANICI (UE) KONUMUNDA hesaplanır:
//
//	SINR_i = S_i / ( Σ_j I_j + N0·B )
//
//	S_i : servis BS'ten UE'ye gelen güç              = Ptx · G0 · d_iU^-α · χ_i
//	I_j : aynı PRB'yi kullanan, HİZMETTEKİ komşu j'den
//	      UE'ye gelen güç                            = Ptx · G0 · d_jU^-α · χ_ij
//
// HATA 4 DÜZELTMELERİ:
//  (1) COMMITTED olamayan (FAILED) istasyon yayında değildir: kullanıcısı
//      0 Mbps alır ve ortalamayı ŞİŞİRMEZ (eskiden girişimsiz sayılıp
//      en yüksek hızı alıyordu).
//  (2) FAILED komşu yayın yapamayacağı için girişim de ÜRETEMEZ.
//
// HATA 5 DÜZELTMELERİ:
//  (1) Girişim, komşu BS'ten KULLANICIYA olan gerçek geometrik mesafeden
//      hesaplanır (eskiden BS-BS link ağırlığı kullanılıyordu).
//  (2) Servis linkinde de log-normal gölgeleme vardır (simetri).
//  (3) SINR ≤ ~30 dB ve SE ≤ 8 bps/Hz tavanları (fiziksel gerçekçilik;
//      eski model 500 Mbps'e varan tavansız hızlar üretebiliyordu).
//  (4) RNG artık PARAMETRE: time.Now() tohumu kaldırıldı, -seed verildiğinde
//      throughput/fairness de tekrarlanabilir.
//  (5) Eski "cell shrinkage" (girişim arttıkça kullanıcıyı BS'e yaklaştırma)
//      kaldırıldı: kötü tahsisin cezasını modelden silen ters nedensellikti.
//
// HATA 6 DÜZELTMESİ — DONMUŞ KANAL GERÇEKLEMESİ:
// Kanalın TÜM stokastik bileşenleri (UE konumları, servis gölgelemesi,
// her komşu link için girişim gölgelemesi) koşu başına BİR KEZ çekilir
// (DrawChannel) ve bütün tahsis şemaları (dağıtık NE, greedy, DSATUR,
// sabit reuse, rastgele) AYNI gerçekleme üzerinde değerlendirilir
// (ComputeThroughputs). Böylece şemalar arasındaki fairness/hız FARKI
// yalnızca tahsis kararından kaynaklanır; kullanıcı yerleşiminin şansı
// ortak paydada sadeleşir.

// Channel: bir koşunun donmuş kanal gerçeklemesi (ağ dizisi sırasıyla indeksli).
type Channel struct {
	UEX, UEY     []float64              // her istasyonun kullanıcısının konumu
	DServ        []float64              // UE'nin servis BS'ine uzaklığı
	ServShadow   []float64              // servis linki gölgelemesi χ_i
	InterfShadow []map[Agent_ID]float64 // [i][j]: komşu j -> i'nin UE'si linki gölgelemesi χ_ij
}

// lognormalShadow: log-normal gölgeleme çarpanı χ = exp(σ·X), X ~ N(0,1).
// Topoloji kurulumu (BuildNetwork) ve fiziksel katman AYNI modeli kullanır.
func lognormalShadow(rng *rand.Rand) float64 {
	return math.Exp(rng.NormFloat64() * ShadowSigma)
}

// pathGain: log-uzaklık yol kaybı kazancı, G0 · d^-α (d0 = 1 m referans).
func pathGain(d float64) float64 {
	if d < 1.0 {
		d = 1.0 // referans mesafenin altına inme
	}
	return ReferenceLoss * math.Pow(d, -PathLossExponent)
}

// indexOf: Agent_ID -> ağ dizisi indeksi.
func indexOf(network []*BaseStation) map[Agent_ID]int {
	idx := make(map[Agent_ID]int, len(network))
	for i, bs := range network {
		idx[bs.ID] = i
	}
	return idx
}

// DrawChannel: koşunun tüm kanal rastgeleliğini BİR KEZ çeker.
func DrawChannel(network []*BaseStation, rng *rand.Rand) *Channel {
	n := len(network)
	ch := &Channel{
		UEX: make([]float64, n), UEY: make([]float64, n),
		DServ:        make([]float64, n),
		ServShadow:   make([]float64, n),
		InterfShadow: make([]map[Agent_ID]float64, n),
	}
	for i, bs := range network {
		d := UEMinDist + rng.Float64()*(UEMaxDist-UEMinDist)
		theta := rng.Float64() * 2 * math.Pi
		ch.DServ[i] = d
		ch.UEX[i] = bs.X + d*math.Cos(theta)
		ch.UEY[i] = bs.Y + d*math.Sin(theta)
		ch.ServShadow[i] = lognormalShadow(rng)
		ch.InterfShadow[i] = make(map[Agent_ID]float64, len(bs.NeighborWeights))
		for nid := range bs.NeighborWeights {
			ch.InterfShadow[i][nid] = lognormalShadow(rng)
		}
	}
	return ch
}

// ComputeThroughputs: verilen renk ataması (assign) ve hizmet vektörü
// (served; false => istasyon yayında değil, Hata 4) için, donmuş kanal
// üzerinde her istasyonun kullanıcı hızını (Mbps) döndürür.
func ComputeThroughputs(network []*BaseStation, assign []PRB, served []bool, ch *Channel) []float64 {
	idx := indexOf(network)
	out := make([]float64, len(network))

	for i := range network {
		// HATA 4 (1/2): hizmette olmayan istasyonun kullanıcısı 0 Mbps.
		if !served[i] {
			out[i] = 0
			continue
		}

		signalPower := TxPowerWatts * pathGain(ch.DServ[i]) * ch.ServShadow[i]

		// HATA 4 (2/2) + HATA 5 (1): girişim yalnızca fiilen yayında olan
		// (served) aynı-renk komşulardan, interferer -> UE mesafesi üzerinden.
		interferencePower := 0.0
		for nid, shadow := range ch.InterfShadow[i] {
			j := idx[nid]
			if !served[j] || assign[j] != assign[i] {
				continue
			}
			nb := network[j]
			dJU := math.Hypot(nb.X-ch.UEX[i], nb.Y-ch.UEY[i])
			interferencePower += TxPowerWatts * pathGain(dJU) * shadow
		}

		// HATA 5 (3): SINR ve spektral verim tavanları.
		sinr := signalPower / (interferencePower + NoiseWatts)
		if sinr > MaxSINR {
			sinr = MaxSINR
		}
		se := math.Log2(1 + sinr)
		if se > MaxSpectralEff {
			se = MaxSpectralEff
		}
		out[i] = BandwidthHz * se / 1e6 // Mbps
	}
	return out
}

// NEAssignment: dağıtık koşunun ürettiği sonucu (renk + hizmet durumu)
// atama vektörlerine çevirir. COMMITTED olamayan istasyon hizmet dışıdır.
func NEAssignment(network []*BaseStation) (assign []PRB, served []bool) {
	assign = make([]PRB, len(network))
	served = make([]bool, len(network))
	for i, bs := range network {
		assign[i] = bs.CurrentPRB
		served[i] = bs.State == STATE_COMMITTED
	}
	return assign, served
}

// EvaluateNetworkThroughput: tek koşu / görselleştirme uyumluluğu —
// kanal çeker, NE'yi değerlendirir, sonuçları bs.Throughput'a yazar
// ve kanalı döndürür (baseline kıyasları aynı kanalı kullanabilsin diye).
func EvaluateNetworkThroughput(network []*BaseStation, rng *rand.Rand) *Channel {
	ch := DrawChannel(network, rng)
	assign, served := NEAssignment(network)
	thr := ComputeThroughputs(network, assign, served, ch)
	for i, bs := range network {
		bs.Throughput = thr[i]
	}
	return ch
}
