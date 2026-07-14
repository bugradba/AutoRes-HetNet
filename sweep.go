package main

import (
	"fmt"
	"math/rand"
	"os"
)

// ============================================================
// YAKINSAMA TARAMASI (K × N ızgarası) — projenin asıl katkısının
// (asenkron protokolün yakınsama davranışı) sistematik ölçümü.
//
// Her (K, N) hücresi için 'runsPer' bağımsız koşu yapılır ve şunlar
// ölçülür: yakınsama oranı ve süresi (PROTOKOL TURU cinsinden — sabit
// zamanlayıcıların duvar saatini domine etmesi sorununu aşar), mesaj
// karmaşıklığı (istasyon başına), CONFLICT ve düşen mesaj sayıları,
// sıfır-girişim oranı (K'nin kromatik sayıya yetip yetmediğinin izi).
//
// Ham koşu verileri CSV'ye yazılır; makaledeki yakınsama-süresi
// CDF'leri doğrudan bu dosyadan çizilir (plot_sweep.py).
//
// NOT: Tarama, -timescale ile küçültülmüş zamanlayıcılarla çalıştırılmak
// üzere tasarlandı (örn. 0.1). Zamanlayıcı ORANLARI korunduğundan
// protokol dinamiği niteliksel olarak aynı kalır; protokol-turu ve mesaj
// metrikleri zaten ölçekten bağımsızdır.
// ============================================================

func RunSweep(runsPer int, baseSeed int64, csvPath string, Ks, Ns []int) {
	f, err := os.Create(csvPath)
	if err != nil {
		fmt.Printf("CSV açılamadı (%v); yalnızca özet basılacak.\n", err)
		f = nil
	} else {
		defer f.Close()
		fmt.Fprintln(f, "K,N,run,seed,converged,conv_sec,conv_rounds,committed_frac,avg_degree,msgs_per_bs,proposes,conflicts,dropped,interference,zero_interf,avg_mbps_served,jain,cfl_converged,cfl_rounds,cfl_cost")
	}

	origK := MaxColors
	defer func() { MaxColors = origK }()

	fmt.Printf("--- YAKINSAMA TARAMASI: K=%v x N=%v, hücre başına %d koşu ---\n", Ks, Ns, runsPer)
	fmt.Printf("(ThinkPeriod=%v, CommitTimeout=%v; 1 protokol turu = 1 ThinkPeriod)\n\n", ThinkPeriod, CommitTimeout)
	fmt.Printf("%-4s %-4s | %-8s | %-16s | %-12s | %-10s | %-8s | %s\n",
		"K", "N", "conv%", "tur (ort±GA)", "msg/BS", "CONFLICT", "drop", "sıfır%")

	cfg := 0
	for _, K := range Ks {
		for _, N := range Ns {
			MaxColors = K

			var rounds, msgs, cnfs, drops, cflRounds []float64
			convCount, zeroCount := 0, 0

			for r := 0; r < runsPer; r++ {
				seed := baseSeed + int64(cfg)*100000 + int64(r)
				rng := rand.New(rand.NewSource(seed))
				net := BuildNetwork(rng, N, SimAreaSize, SimThreshold, false)

				degSum := 0
				for _, bs := range net {
					degSum += len(bs.Neighbros)
				}
				avgDeg := float64(degSum) / float64(N)

				maxWait := 40 * ThinkPeriod // ölçekle uyumlu üst sınır (~20 s @ ölçek 1)
				convSec, converged := RunSimulation(net, maxWait)
				convR := convSec / ThinkPeriod.Seconds()

				ms := CollectMessageStats(net)
				perBS := float64(ms.Total) / float64(N)

				obj := CalculateGlobalObjective(net)
				zero := obj < 1e-15
				commFrac := float64(CommittedCount(net)) / float64(N)

				ch := DrawChannel(net, rng)
				assign, served := NEAssignment(net)
				thr := ComputeThroughputs(net, assign, served, ch)
				avgMbps := meanServed(thr, served)
				jain := JainOf(thr)

				// CFL karşılaştırması: aynı topolojide, ayrık rng ile
				// (protokolün rng tüketimini etkilemesin diye türetilmiş tohum).
				cflRng := rand.New(rand.NewSource(seed*31 + 7))
				cflAssign, cflR, cflOK := RunCFL(net, K, CFLDefaultB, CFLMaxRounds, cflRng)
				cflCost := AssignmentCost(net, cflAssign)
				if cflOK {
					cflRounds = append(cflRounds, float64(cflR))
				}

				if converged {
					convCount++
					rounds = append(rounds, convR)
				}
				if zero {
					zeroCount++
				}
				msgs = append(msgs, perBS)
				cnfs = append(cnfs, float64(ms.Conflicts))
				drops = append(drops, float64(ms.Dropped))

				if f != nil {
					fmt.Fprintf(f, "%d,%d,%d,%d,%t,%.4f,%.3f,%.4f,%.3f,%.3f,%d,%d,%d,%.6e,%t,%.3f,%.4f,%t,%d,%.6e\n",
						K, N, r, seed, converged, convSec, convR, commFrac, avgDeg,
						perBS, ms.Proposes, ms.Conflicts, ms.Dropped, obj, zero, avgMbps, jain,
						cflOK, cflR, cflCost)
				}
			}

			fmt.Printf("%-4d %-4d | %6.0f%%  | %6.2f ± %-6.2f | %5.1f ± %-4.1f | %8.1f | %6.1f | %5.0f%% | CFL: %5.1f tur (%.0f%%)\n",
				K, N, 100*float64(convCount)/float64(runsPer),
				mean(rounds), ci95Half(rounds),
				mean(msgs), ci95Half(msgs),
				mean(cnfs), mean(drops),
				100*float64(zeroCount)/float64(runsPer),
				mean(cflRounds), 100*float64(len(cflRounds))/float64(runsPer))
			cfg++
		}
	}

	if f != nil {
		fmt.Printf("\nHam koşu verileri yazıldı: %s (CDF'ler için: python plot_sweep.py %s sweep)\n", csvPath, csvPath)
	}
}
