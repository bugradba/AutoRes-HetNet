package main

import (
	"math"
	"math/rand"
	"time"
)

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
	// Rastgele seed'i istasyon ID'sine göre belirle
	rSource := rand.NewSource(time.Now().UnixNano() + int64(bs.ID)*100)
	rGen := rand.New(rSource)

	// Önce girişim seviyesini hesapla
	interferencePower := 0.0
	interferenceCount := 0
	totalInterferenceWeight := 0.0

	if bs.State == STATE_COMMITTED {
		for neighborID, neighborColor := range bs.NeighborMap {
			if neighborColor == bs.CurrentPRB {
				h_ji := bs.NeighborWeights[neighborID]
				interferencePower += (TxPowerWatts * h_ji)
				interferenceCount++
				totalInterferenceWeight += h_ji
			}
		}
	}

	// Girişim durumuna göre kullanıcı mesafesini belirle
	minDist := 10.0
	maxDist := 100.0 // 300 çok fazlaydı, düşürelim

	// Girişim varsa kullanıcı daha yakın olmalı (cell shrinkage)
	if interferenceCount > 0 {
		// Ağır girişim varsa maksimum mesafeyi kısalt
		interferenceRatio := math.Min(totalInterferenceWeight/1e-6, 1.0)
		maxDist = 100.0 - (interferenceRatio * 50.0) // 50-100m arası
	} else {
		maxDist = 150.0 // Girişim yoksa daha geniş kapsama
	}

	actualUserDist := minDist + rGen.Float64()*(maxDist-minDist)

	// Sinyal hesabı
	signalGain := ReferenceLoss * math.Pow(actualUserDist, -PathLossExponent)
	signalPower := TxPowerWatts * signalGain

	// SINR ve kapasite
	sinr := signalPower / (interferencePower + NoiseWatts)
	capacity := BandwidthHz * math.Log2(1+sinr) / 1e6

	bs.Throughput = capacity
}
