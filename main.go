package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"
)

func main() {
	// HATA 3 DÜZELTMESİ: varsayılan mod artık Monte Carlo.
	// Tek bir stokastik koşu temsil edici olmadığı için sayılar
	// yalnızca çok-koşulu ortalama ± güven aralığı olarak savunulabilir.

	runs := flag.Int("runs", 100, "Monte Carlo koşu sayısı (1 = eski tarz ayrıntılı tek koşu); sweep modunda hücre başına koşu")
	seed := flag.Int64("seed", 42, "temel tohum; koşu r'nin tohumu = seed + r (tekrarlanabilirlik)")
	verbose := flag.Bool("v", false, "ajan mesajlarını yazdır (yalnızca -runs 1 için önerilir)")
	optBudget := flag.Duration("optbudget", 3*time.Second, "koşu başına B&B optimum zaman bütçesi")

	// Y-1: README'nin belgeleyip kodda bulunmayan bayraklar
	sweep := flag.Bool("sweep", false, "K x N yakınsama taramasını çalıştır")
	sweepK := flag.String("sweepK", "3,4,5,6", "tarama K değerleri (virgülle ayrık)")
	sweepN := flag.String("sweepN", "20,40,60,80", "tarama N değerleri (virgülle ayrık)")
	timescale := flag.Float64("timescale", 1.0, "tüm protokol zamanlayıcılarını ölçekler (0.1 = 10x hızlı; oranlar korunur)")
	csvPath := flag.String("csv", "sweep_results.csv", "sweep ham veri çıktısı (koşu başına satır)")
	flag.Parse()

	if *timescale <= 0 {
		fmt.Println("HATA: -timescale pozitif olmalı")
		os.Exit(1)
	}
	SetTimescale(*timescale)

	if *sweep {
		Verbose = false
		Ks, err := ParseIntList(*sweepK)
		if err != nil {
			fmt.Println("HATA: -sweepK:", err)
			os.Exit(1)
		}
		Ns, err := ParseIntList(*sweepN)
		if err != nil {
			fmt.Println("HATA: -sweepN:", err)
			os.Exit(1)
		}
		if err := RunSweep(Ks, Ns, *runs, *seed, *csvPath); err != nil {
			fmt.Println("HATA: sweep:", err)
			os.Exit(1)
		}
		return
	}

	if *runs > 1 {
		Verbose = false // yüzlerce koşuda ajan logları hem okunmaz hem yavaşlatır
		RunMonteCarlo(*runs, *seed, *optBudget)
		return
	}

	// ---- TEK KOŞU (ayrıntılı / eğitim modu) ----
	Verbose = *verbose
	rng := rand.New(rand.NewSource(*seed))

	fmt.Println("--- 5G Distributed Resource Management Simulation Commences ---")
	fmt.Printf("(tek koşu, seed=%d — sayılar TEK örneklemdir, genelleme için -runs kullanın)\n", *seed)

	Network := BuildNetwork(rng, SimN, SimAreaSize, SimThreshold, Verbose)
	numDevice := len(Network)

	// ESKİ: time.Sleep(15 * time.Second) — duvar saati, yakınsama koşulu değil.
	// YENİ: mantıksal yakınsama (tüm istasyonlar COMMITTED) + güvenlik üst sınırı.
	convSec, converged := RunSimulation(Network, MaxWait)

	fmt.Println("\n--- Simulation completed ---")
	fmt.Printf("Yakınsama: %v | Süre: %.2f s (%.1f protokol turu) | COMMITTED: %d/%d\n",
		converged, convSec, convSec/ThinkPeriod.Seconds(), CommittedCount(Network), numDevice)
	mst := CollectMessageStats(Network)
	fmt.Printf("Mesajlar: %d gönderildi (%.1f/istasyon) | %d düştü | %d CONFLICT\n",
		mst.Sent, float64(mst.Sent)/float64(numDevice), mst.Dropped, mst.Conflicts)

	// H-2 denetimi: nihai tahsis GERÇEKTEN Nash dengesi mi?
	// (Her istasyonun gerçek nihai komşu renklerine göre en-iyi-yanıtı
	// yeniden hesaplanır; sapmak isteyen istasyon sayısı 0 olmalıdır.)
	viol, uncomm := VerifyNashEquilibrium(Network)
	if viol == 0 && uncomm == 0 {
		fmt.Println("Nash denetimi: 0 ihlal -> nihai tahsis dogrulanmis bir Nash dengesidir.")
	} else {
		fmt.Printf("Nash denetimi: %d istasyon sapmak istiyor, %d istasyon uncommitted -> NASH DENGESI DEGIL!\n", viol, uncomm)
	}
	fmt.Println("--- FINAL RESULTS ---")

	for _, bs := range Network {
		status := "FAILED"
		if bs.State == STATE_COMMITTED {
			status = fmt.Sprintf(" PRB-%d", bs.CurrentPRB)
		}
		fmt.Printf("BS-%d: %s (Neighbors: %d)\n", bs.ID, status, len(bs.Neighbros))

	}

	// ----------------------------------------------------

	fmt.Println("\n--- CALCULATING NETWORK THROUGHPUT (SHANNON CAPACITY) ---")

	totalNetworkCapacity := 0.0

	for _, bs := range Network {
		bs.CalculateShannonCapacity(Network) // Hesapla ve Struct'a kaydet
		totalNetworkCapacity += bs.Throughput

		fmt.Printf("BS-%d | Color: %d | Throughput: %.2f Mbps\n", bs.ID, bs.CurrentPRB, bs.Throughput)
	}

	fairnessScore := CalculateJainsFairness(Network)
	globalObjective := CalculateGlobalObjective(Network)

	fmt.Printf("\n>>> SYSTEM PERFORMANCE RESULTS V1 <<<\n")
	fmt.Printf("1. Total Network Capacity : %.2f Mbps (Higher is better)\n", totalNetworkCapacity)
	fmt.Printf("2. Average User Speed     : %.2f Mbps\n", totalNetworkCapacity/float64(numDevice))

	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("\n>>> SYSTEM PERFORMANCE V2 <<<\n")
	fmt.Printf("Jain's Fairness Index: %.4f (1.0 = Perfect Fairness)\n", fairnessScore)
	if globalObjective > 1e-15 {
		fmt.Printf("Global Objective (Total Interference): %.3e (%.2f dBm equivalent)\n",
			globalObjective, 10*math.Log10(globalObjective/1e-3))
	} else {
		fmt.Printf("Global Objective (Total Interference): < 1e-15 (Virtually Zero)\n")
	}

	// --- 1) GREEDY REFERANSA GÖRE KAZANIM (eski "PoA" metriğinin dürüst adı) ---
	greedyCost := CalculateGreedyBaseline(Network)
	epsilon := 1e-15

	if greedyCost > epsilon {
		fmt.Printf("Greedy Baseline (Centralized Heuristic): %.3e (%.2f dBm equivalent)\n",
			greedyCost, 10*math.Log10(greedyCost/1e-3))
	} else {
		fmt.Printf("Greedy Baseline (Centralized Heuristic): < 1e-15 (Virtually Zero)\n")
	}

	switch {
	case globalObjective < epsilon && greedyCost < epsilon:
		fmt.Printf(">>> GAIN OVER GREEDY              : 1.0000 (her ikisi de ~sıfır maliyet)\n")
	case greedyCost < epsilon:
		fmt.Printf(">>> GAIN OVER GREEDY              : tanımsız (greedy ~0, dağıtık > 0)\n")
	default:
		gain := globalObjective / greedyCost
		fmt.Printf(">>> GAIN OVER GREEDY              : %.4f (<1: dağıtık daha iyi, >1: greedy daha iyi)\n", gain)
	}
	fmt.Println("(Not: Bu oran PoA DEGILDIR; payda sezgisel greedy'dir, gercek optimum degil.)")
	fmt.Println("------------------------------------------------------------------")

	// --- 2) GERÇEK OPTİMUM ve EMPİRİK PoA ALT SINIRI ---
	// PoA = (en kötü NE) / (gerçek optimum). Tek koşuda tek NE gözlediğimiz
	// için buradaki oran PoA'nın kendisi değil, bir ALT SINIRIDIR.
	fmt.Println("\n--- TRUE OPTIMUM (Branch-and-Bound) ---")
	optStart := time.Now()
	opt := BruteForceOptimum(Network, int(MaxColors), 10*time.Second)
	fmt.Printf("B&B süresi: %.2fs | Kanıtlanmış optimum: %v\n", time.Since(optStart).Seconds(), opt.Exact)

	if !opt.Exact {
		fmt.Println("Zaman bütçesi aşıldı: bulunan değer yalnızca ÜST SINIRDIR, PoA raporlanamaz.")
		fmt.Printf("En iyi bulunan maliyet (üst sınır): %.3e\n", opt.Cost)
	} else {
		if opt.Cost > epsilon {
			fmt.Printf("True Social Optimum               : %.3e (%.2f dBm equivalent)\n",
				opt.Cost, 10*math.Log10(opt.Cost/1e-3))
		} else {
			fmt.Printf("True Social Optimum               : 0 (graf %d renkle uygun renklenebilir)\n", MaxColors)
		}

		switch {
		case globalObjective < epsilon && opt.Cost < epsilon:
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT) : 1.0000 (bu NE optimuma eşit)\n")
		case opt.Cost < epsilon:
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT) : +Inf (optimum 0, bu NE > 0)\n")
		default:
			ratio := globalObjective / opt.Cost
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT) : %.4f (tanım gereği >= 1; gerçek PoA >= bu değer)\n", ratio)
		}
	}
	fmt.Println("------------------------------------------------------------------")

	fmt.Println("\n--- DETAILED INTERFERENCE CHECK ---")
	totalRawInterference := 0.0
	for _, bs := range Network {
		if bs.CurrentPRB == -1 {
			continue
		}

		localInterference := 0.0
		for neighborID, weight := range bs.NeighborWeights {
			// Komşunun rengini bul
			for _, neighbor := range Network {
				if neighbor.ID == neighborID && neighbor.CurrentPRB == bs.CurrentPRB {
					localInterference += weight
					totalRawInterference += weight
					fmt.Printf("BS-%d <-> BS-%d | Both use Color %d | Weight: %.3e\n",
						bs.ID, neighborID, bs.CurrentPRB, weight)
					break
				}
			}
		}
	}
	fmt.Printf("Raw Total Interference: %.3e\n", totalRawInterference)
	fmt.Printf("Divided by 2: %.3e\n", totalRawInterference/2.0)

	type VizData struct {
		Nodes []struct {
			ID    int     `json:"id"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Color int     `json:"color"`
		} `json:"nodes"`
		Edges []struct {
			Source int `json:"source"`
			Target int `json:"target"`
		} `json:"edges"`
	}

	data := VizData{}
	for _, bs := range Network {
		// Düğümleri ekle
		data.Nodes = append(data.Nodes, struct {
			ID    int     `json:"id"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Color int     `json:"color"`
		}{
			ID:    int(bs.ID),
			X:     bs.X,
			Y:     bs.Y,
			Color: int(bs.CurrentPRB),
		})

		for _, neighborID := range bs.Neighbros {
			if bs.ID < neighborID { // Her kenarı bir kez eklemek için
				data.Edges = append(data.Edges, struct {
					Source int `json:"source"`
					Target int `json:"target"`
				}{
					Source: int(bs.ID),
					Target: int(neighborID),
				})
			}
		}
	}

	file, _ := json.MarshalIndent(data, "", " ")
	_ = os.WriteFile("viz_data.json", file, 0644)
	fmt.Println(" The data has been saved to the “viz_data.json” file.")
}
