package main

import (
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

// ============================================================
// Y-1 DÜZELTMESİ: K x N YAKINSAMA TARAMASI
//
// README'nin belgeleyip depoda bulunmayan ikinci dosya. Renk sayısı
// (K) ve istasyon sayısı (N) ızgarası üzerinde, hücre başına 'runs'
// bağımsız koşu yapar; ham veriyi koşu-satırı olarak CSV'ye yazar
// (CDF'ler ve ölçekleme grafikleri plot_sweep.py ile bu dosyadan
// üretilir) ve hücre özetlerini ekrana basar.
//
// Yakınsama süresi PROTOKOL TURU (think-period) cinsindendir; bu birim
// -timescale'den bağımsızdır, dolayısıyla hızlandırılmış (ör. 0.1)
// taramaların sonuçları tam hızla karşılaştırılabilir.
// ============================================================

// ParseIntList: "3,4,5,6" -> [3 4 5 6]. Sweep bayrakları için.
func ParseIntList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("geçersiz liste öğesi %q: %w", p, err)
		}
		if v <= 0 {
			return nil, fmt.Errorf("liste öğeleri pozitif olmalı: %d", v)
		}
		out = append(out, v)
	}
	return out, nil
}

// RunSweep: her (K, N) hücresi için 'runsPerCell' koşu yapar.
// Koşu r'nin tohumu = baseSeed + r: aynı N için topoloji K'den
// bağımsızdır, yani K etkisi kanal/yerleşim şansından izole edilir.
func RunSweep(Ks, Ns []int, runsPerCell int, baseSeed int64, csvPath string) error {
	f, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("CSV açılamadı: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"k", "n", "run", "seed", "converged", "conv_rounds", "conv_seconds",
		"committed_frac", "interference_w", "ne_violations",
		"msgs_per_station", "drops_total", "conflicts_total", "zero_interference",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	const eps = 1e-15
	origK := MaxColors
	defer func() { MaxColors = origK }()

	fmt.Printf("--- SWEEP: K=%v x N=%v, hücre başına %d koşu (baseSeed=%d, timescale ThinkPeriod=%v) ---\n\n",
		Ks, Ns, runsPerCell, baseSeed, ThinkPeriod)

	for _, k := range Ks {
		for _, n := range Ns {
			MaxColors = PRB(k)

			var (
				rounds    []float64
				convCount int
				zeroCount int
				neOK      int
				msgAvg    []float64
				dropSum   int64
				confSum   int64
			)

			for r := 0; r < runsPerCell; r++ {
				seed := baseSeed + int64(r)
				rng := rand.New(rand.NewSource(seed))
				net := BuildNetwork(rng, n, SimAreaSize, SimThreshold, false)

				convSec, converged := RunSimulation(net, MaxWait)
				obj := CalculateGlobalObjective(net)
				viol, uncomm := VerifyNashEquilibrium(net)
				mst := CollectMessageStats(net)
				commFrac := float64(CommittedCount(net)) / float64(n)
				convRound := convSec / ThinkPeriod.Seconds()

				if converged {
					convCount++
					rounds = append(rounds, convRound)
				}
				if obj < eps {
					zeroCount++
				}
				if viol == 0 && uncomm == 0 {
					neOK++
				}
				msgAvg = append(msgAvg, float64(mst.Sent)/float64(n))
				dropSum += mst.Dropped
				confSum += mst.Conflicts

				row := []string{
					strconv.Itoa(k), strconv.Itoa(n), strconv.Itoa(r),
					strconv.FormatInt(seed, 10),
					strconv.FormatBool(converged),
					fmt.Sprintf("%.3f", convRound),
					fmt.Sprintf("%.3f", convSec),
					fmt.Sprintf("%.4f", commFrac),
					fmt.Sprintf("%.6e", obj),
					strconv.Itoa(viol + uncomm),
					fmt.Sprintf("%.2f", float64(mst.Sent)/float64(n)),
					strconv.FormatInt(mst.Dropped, 10),
					strconv.FormatInt(mst.Conflicts, 10),
					strconv.FormatBool(obj < eps),
				}
				if err := w.Write(row); err != nil {
					return err
				}
			}
			w.Flush()

			fmt.Printf("K=%d N=%3d | conv %3d/%d | tur %6.1f ± %4.1f | NE %3d/%d | sıfır-girişim %3.0f%% | msg/bs %5.1f | drop %d | conflict %d\n",
				k, n, convCount, runsPerCell, mean(rounds), ci95Half(rounds),
				neOK, runsPerCell, 100*float64(zeroCount)/float64(runsPerCell),
				mean(msgAvg), dropSum, confSum)
		}
	}

	fmt.Printf("\nHam veri yazıldı: %s (grafikler: python plot_sweep.py %s <prefix>)\n", csvPath, csvPath)
	return nil
}
