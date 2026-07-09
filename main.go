package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

// parseIntList: "3,4,5" -> []int{3,4,5}
func parseIntList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func main() {
	// HATA 3: varsayılan mod Monte Carlo — tek stokastik koşu temsil
	// edici olmadığı için sayılar ortalama ± GA olarak savunulur.
	runs := flag.Int("runs", 100, "Monte Carlo koşu sayısı (1 = ayrıntılı tek koşu); sweep modunda hücre başına koşu")
	seed := flag.Int64("seed", 42, "temel tohum; koşu r'nin tohumu = seed + r (tekrarlanabilirlik)")
	verbose := flag.Bool("v", false, "ajan mesajlarını yazdır (yalnızca -runs 1 için önerilir)")
	optBudget := flag.Duration("optbudget", 3*time.Second, "koşu başına B&B optimum zaman bütçesi")
	sweep := flag.Bool("sweep", false, "K x N yakınsama taramasını çalıştır")
	sweepK := flag.String("sweepK", "3,4,5,6", "tarama K değerleri (virgülle)")
	sweepN := flag.String("sweepN", "20,40,60,80", "tarama N değerleri (virgülle)")
	timescale := flag.Float64("timescale", 1.0, "tüm protokol zamanlayıcılarını orantılı ölçekle (0.1 = 10x hızlı)")
	csvPath := flag.String("csv", "sweep_results.csv", "tarama ham verisinin yazılacağı CSV")
	flag.Parse()

	if *timescale != 1.0 {
		SetTimeScale(*timescale)
		fmt.Printf("(timescale=%.3g: ThinkPeriod=%v, CommitTimeout=%v — oranlar korunur)\n",
			*timescale, ThinkPeriod, CommitTimeout)
	}

	if *sweep {
		Verbose = false
		Ks, err1 := parseIntList(*sweepK)
		Ns, err2 := parseIntList(*sweepN)
		if err1 != nil || err2 != nil {
			fmt.Println("HATA: -sweepK / -sweepN 'a,b,c' biçiminde tamsayı listesi olmalı")
			os.Exit(1)
		}
		RunSweep(*runs, *seed, *csvPath, Ks, Ns)
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

	convSec, converged := RunSimulation(Network, 40*ThinkPeriod)
	convRounds := convSec / ThinkPeriod.Seconds()

	fmt.Println("\n--- Simulation completed ---")
	fmt.Printf("Yakınsama: %v | Süre: %.2f s (%.1f protokol turu) | COMMITTED: %d/%d\n",
		converged, convSec, convRounds, CommittedCount(Network), numDevice)

	ms := CollectMessageStats(Network)
	fmt.Printf("Mesajlar: toplam %d (%.1f/istasyon) | PROPOSE %d | CONFLICT %d | düşen %d\n",
		ms.Total, float64(ms.Total)/float64(numDevice), ms.Proposes, ms.Conflicts, ms.Dropped)

	fmt.Println("--- FINAL RESULTS ---")
	for _, bs := range Network {
		status := "FAILED"
		if bs.State == STATE_COMMITTED {
			status = fmt.Sprintf(" PRB-%d", bs.CurrentPRB)
		}
		fmt.Printf("BS-%d: %s (Neighbors: %d)\n", bs.ID, status, len(bs.Neighbros))
	}

	// ---- FİZİKSEL KATMAN (donmuş kanal) ----
	fmt.Println("\n--- CALCULATING NETWORK THROUGHPUT (FROZEN CHANNEL / CAPPED SHANNON) ---")
	ch := EvaluateNetworkThroughput(Network, rng)
	neAssign, neServed := NEAssignment(Network)

	totalNetworkCapacity := 0.0
	for _, bs := range Network {
		totalNetworkCapacity += bs.Throughput
		fmt.Printf("BS-%d | Color: %d | Throughput: %.2f Mbps\n", bs.ID, bs.CurrentPRB, bs.Throughput)
	}

	fairnessScore := CalculateJainsFairness(Network)
	globalObjective := CalculateGlobalObjective(Network)

	fmt.Printf("\n>>> SYSTEM PERFORMANCE RESULTS V1 <<<\n")
	fmt.Printf("1. Total Network Capacity : %.2f Mbps\n", totalNetworkCapacity)
	fmt.Printf("2. Avg User Speed (served): %.2f Mbps (FAILED istasyonlar paydaya girmez)\n",
		meanServed(func() []float64 {
			xs := make([]float64, numDevice)
			for i, bs := range Network {
				xs[i] = bs.Throughput
			}
			return xs
		}(), neServed))

	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("\n>>> SYSTEM PERFORMANCE V2 <<<\n")
	fmt.Printf("Jain's Fairness Index: %.4f (1.0 = Perfect Fairness)\n", fairnessScore)
	if globalObjective > 1e-15 {
		fmt.Printf("Global Objective (Total Interference): %.3e (%.2f dBm equivalent)\n",
			globalObjective, 10*math.Log10(globalObjective/1e-3))
	} else {
		fmt.Printf("Global Objective (Total Interference): < 1e-15 (Virtually Zero)\n")
	}

	// ---- ŞEMA KARŞILAŞTIRMASI (aynı donmuş kanal) ----
	allServed := make([]bool, numDevice)
	for i := range allServed {
		allServed[i] = true
	}
	fmt.Println("\n--- ŞEMA KARŞILAŞTIRMASI (aynı topoloji + aynı donmuş kanal) ---")
	fmt.Printf("%-14s | %-14s | %-12s | %s\n", "Şema", "Maliyet", "Mbps/served", "Jain")
	for _, sc := range []struct {
		name   string
		assign []PRB
		served []bool
	}{
		{"Dağıtık (NE)", neAssign, neServed},
		{"Greedy", GreedyAssignment(Network), allServed},
		{"DSATUR", DSATURAssignment(Network), allServed},
		{"Sabit reuse", FixedReuseAssignment(Network, MaxColors), allServed},
		{"Rastgele", RandomAssignment(Network, MaxColors, rng), allServed},
	} {
		thr := ComputeThroughputs(Network, sc.assign, sc.served, ch)
		fmt.Printf("%-14s | %12.3e | %10.1f | %.3f\n",
			sc.name, AssignmentCost(Network, sc.assign), meanServed(thr, sc.served), JainOf(thr))
	}

	// --- Gain over Greedy (eski "PoA" metriğinin dürüst adı) ---
	greedyCost := CalculateGreedyBaseline(Network)
	epsilon := 1e-15
	switch {
	case globalObjective < epsilon && greedyCost < epsilon:
		fmt.Printf("\n>>> GAIN OVER GREEDY: 1.0000 (her ikisi de ~sıfır maliyet)\n")
	case greedyCost < epsilon:
		fmt.Printf("\n>>> GAIN OVER GREEDY: tanımsız (greedy ~0, dağıtık > 0)\n")
	default:
		fmt.Printf("\n>>> GAIN OVER GREEDY: %.4f (<1: dağıtık daha iyi, >1: greedy daha iyi)\n", globalObjective/greedyCost)
	}
	fmt.Println("(Not: Bu oran PoA DEGILDIR; payda sezgisel greedy'dir, gercek optimum degil.)")

	// --- Gerçek optimum ve empirik PoA alt sınırı ---
	fmt.Println("\n--- TRUE OPTIMUM (Branch-and-Bound) ---")
	optStart := time.Now()
	opt := BruteForceOptimum(Network, MaxColors, *optBudget)
	fmt.Printf("B&B süresi: %.2fs | Kanıtlanmış optimum: %v\n", time.Since(optStart).Seconds(), opt.Exact)

	if !opt.Exact {
		fmt.Println("Zaman bütçesi aşıldı: bulunan değer yalnızca ÜST SINIRDIR, PoA raporlanamaz.")
		fmt.Printf("En iyi bulunan maliyet (üst sınır): %.3e\n", opt.Cost)
	} else {
		if opt.Cost > epsilon {
			fmt.Printf("True Social Optimum: %.3e (%.2f dBm equivalent)\n", opt.Cost, 10*math.Log10(opt.Cost/1e-3))
		} else {
			fmt.Printf("True Social Optimum: 0 (graf %d renkle uygun renklenebilir)\n", MaxColors)
		}
		switch {
		case globalObjective < epsilon && opt.Cost < epsilon:
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT): 1.0000 (bu NE optimuma eşit)\n")
		case opt.Cost < epsilon:
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT): +Inf (optimum 0, bu NE > 0)\n")
		default:
			fmt.Printf(">>> EMPIRICAL PoA (this NE / OPT): %.4f (gerçek PoA >= bu değer)\n", globalObjective/opt.Cost)
		}
	}
	fmt.Println("------------------------------------------------------------------")

	// ---- Görselleştirme verisi ----
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
		data.Nodes = append(data.Nodes, struct {
			ID    int     `json:"id"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Color int     `json:"color"`
		}{ID: int(bs.ID), X: bs.X, Y: bs.Y, Color: int(bs.CurrentPRB)})

		for _, neighborID := range bs.Neighbros {
			if bs.ID < neighborID { // her kenarı bir kez ekle
				data.Edges = append(data.Edges, struct {
					Source int `json:"source"`
					Target int `json:"target"`
				}{Source: int(bs.ID), Target: int(neighborID)})
			}
		}
	}

	file, _ := json.MarshalIndent(data, "", " ")
	_ = os.WriteFile("viz_data.json", file, 0644)
	fmt.Println(" The data has been saved to the \"viz_data.json\" file.")
}
