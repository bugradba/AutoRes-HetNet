package main

import "math/rand"

// ============================================================
// CFL — Communication-Free Learning
//
// Yayımlanmış dağıtık referans şeması:
//   Leith & Clifford, "A self-managed distributed channel selection
//   algorithm for WLAN" (RAWNET/WiOpt, 2006) — algoritma;
//   Duffy, O'Connell & Sapozhnikov, "Complexity analysis of a
//   decentralised graph colouring algorithm" (Inf. Process. Lett. 2008)
//   — graf renklendirme halinde yakınsama kanıtı.
//
// Fikir: hiç mesajlaşma yok. Her istasyon renkler üzerinde bir olasılık
// vektörü tutar; her turda bir renk örnekler; tur sonunda yalnızca
// "çakışma yaşadım mı?" ikili geri bildirimini kullanır:
//   - başarı  -> o renge kilitlen (birim vektör),
//   - çakışma -> o rengin olasılığını (1-b) ile söndür, b kütlesini
//               diğer renklere eşit dağıt.
// Kilit ayrı bir durum değildir: kilitli istasyon sonradan çakışma
// yaşarsa failure kuralı birim vektörü yeniden yumuşatır.
//
// DÜRÜSTLÜK NOTLARI (README'de de belirtilir):
//  1. CFL SENKRON tur varsayımıyla tanımlıdır ve burada öyle koşulur;
//     bizim protokolümüz ise gerçek asenkron zamanlayıcılarla koşar.
//     Tur sayıları bu farkla birlikte okunmalıdır.
//  2. CFL ağırlıksızdır: failure kararı "herhangi bir komşu aynı rengi
//     seçti mi" ikilisidir; kenar ağırlıkları KULLANILMAZ. Karşılaştırma
//     yine adildir çünkü nihai atamanın maliyetini her şema için aynı
//     AssignmentCost ölçer.
//  3. rng parametredir: -seed ile CFL sonuçları (zamanlayıcı içermediği
//     için) makineden bağımsız olarak birebir tekrarlanabilir.
// ============================================================

const (
	CFLDefaultB  = 0.1 // öğrenme oranı b (literatürde tipik 0.1–0.3)
	CFLMaxRounds = 500 // güvenlik tavanı; kromatik sayı > K ise hiç yakınsamaz
)

// RunCFL: CFL'i verilen ağ üzerinde koşar. Dönen assign, AssignmentCost
// ve ComputeThroughputs'a doğrudan verilebilir (şema arayüzü []PRB).
func RunCFL(network []*BaseStation, k int, b float64, maxRounds int, rng *rand.Rand) (assign []PRB, rounds int, converged bool) {
	n := len(network)
	idx := indexOf(network)

	// Olasılık vektörleri: tekdüze başlangıç.
	p := make([][]float64, n)
	for i := range p {
		p[i] = make([]float64, k)
		for c := range p[i] {
			p[i][c] = 1.0 / float64(k)
		}
	}

	choice := make([]PRB, n)
	success := make([]bool, n)

	sample := func(pi []float64) PRB {
		u := rng.Float64()
		acc := 0.0
		for c, pc := range pi {
			acc += pc
			if u < acc {
				return PRB(c)
			}
		}
		return PRB(k - 1) // kayan nokta artığı: son renge yuvarla
	}

	for r := 1; r <= maxRounds; r++ {
		// FAZ 1 — herkes AYNI ANDA örnekler. choice[] tamamen dolmadan
		// başarı hesabına GEÇİLMEZ (senkron tur; tek döngüde birleştirmek
		// yarı-güncel dizi okutur ve algoritmayı sessizce değiştirir).
		for i := range network {
			choice[i] = sample(p[i])
		}

		// FAZ 2 — başarı: hiçbir komşu aynı rengi seçmediyse.
		allOK := true
		for i, bs := range network {
			ok := true
			for nid := range bs.NeighborWeights {
				if choice[idx[nid]] == choice[i] {
					ok = false
					break
				}
			}
			success[i] = ok
			if !ok {
				allOK = false
			}
		}

		// FAZ 3 — güncelleme.
		for i := range network {
			if success[i] {
				for c := range p[i] {
					p[i][c] = 0
				}
				p[i][choice[i]] = 1
			} else {
				cflApplyFailureUpdate(p[i], choice[i], b)
			}
		}

		if allOK {
			out := make([]PRB, n)
			copy(out, choice)
			return out, r, true
		}
	}

	out := make([]PRB, n)
	copy(out, choice)
	return out, maxRounds, false
}

// cflApplyFailureUpdate: çakışma sonrası olasılık güncellemesi.
// Kütle korunumu: (1-b)·Σp + b = 1 (Σp = 1 iken). Ayrı fonksiyon,
// değişmezi (toplam=1, negatif yok) birim testle sabitleyebilmek için.
func cflApplyFailureUpdate(p []float64, chosen PRB, b float64) {
	share := b / float64(len(p)-1)
	for c := range p {
		if PRB(c) == chosen {
			p[c] *= (1 - b)
		} else {
			p[c] = (1-b)*p[c] + share
		}
	}
}
