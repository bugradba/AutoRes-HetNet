# AutoRes-HetNet

**Distributed PRB allocation for ultra-dense 5G networks via an asynchronous, message-passing Graph Coloring Game — implemented in Go.**

Each base station is an independent agent (a goroutine) that selects its own frequency block (PRB / "color") by exchanging real messages with its neighbors only. There is no central controller, no shared memory, and no synchronized rounds — agents race, collide, back off and retry, exactly like a real distributed protocol.

## The problem

In an ultra-dense network, nearby base stations sharing the same frequency block interfere with each other and throughput collapses. The classical fix is a centralized controller that sees the whole network and assigns frequencies — but it becomes a computational and latency bottleneck as the network densifies, and a single point of failure.

AutoRes-HetNet models the problem as a non-cooperative **Graph Coloring Game**: nodes = base stations, edges = interference relationships (weighted by path loss + log-normal shadowing), colors = orthogonal PRBs. Each agent repeatedly plays its **best response** (the color minimizing weighted conflict with its neighbors' last known colors) until the system settles into a Nash equilibrium.

## What is actually new here

The cell-as-player, color-as-frequency idea is well established in the potential-game literature. The contribution of this project is the **implementation and its empirical convergence analysis**:

- **Genuinely asynchronous and message-based.** Agents communicate through a 4-message protocol (`HELLO`, `PROPOSE`, `SUCCESS`, `CONFLICT`) over Go channels, with random start delays, message races, timeouts and queue drops — not the synchronous round-based or "one player moves at a time" abstraction most prior work assumes.
- **Contention resolution inside the protocol.** ID-priority plus random backoff (CSMA-like) breaks livelocks; conflict handling is not delegated to an idealized scheduler.
- **End-to-end physical layer.** The color assignment is carried through a log-distance + log-normal shadowing channel to per-user downlink SINR, capped Shannon capacity, and Jain fairness — not just "conflict counts".

## Key empirical findings

All numbers below come from seeded Monte Carlo experiments with 95% confidence intervals (mean ± 1.96·σ/√n), not single runs.

**1. Convergence takes ~8 protocol rounds, essentially independent of density.** Across a K ∈ {3,4,5,6} × N ∈ {20,40,60,80} sweep (30 runs per cell, fixed 400×400 m area), convergence rate was 100% and convergence time stayed at 8.0–8.1 think-periods — even as the number of `CONFLICT` messages grew ~35× (2 → 71 at K=3). Mechanism: losers of a color contention back off and re-propose *within* the winners' commit-timeout window, so contention is absorbed by the pipeline instead of extending it. **Caveat:** this constancy is tied to the timeout structure and has so far been verified up to N=80; stress regimes (K=2, N up to 200) are the next experiment (`-sweepK "2,3" -sweepN "80,120,160,200"`).

**2. Message complexity is linear in density.** Messages per station grow ~9 → ~38 as N goes 20 → 80 (average degree grows linearly at fixed area). No queue drops were observed at any tested density.

**3. Zero interference is a property of the graph, not of the algorithm.** Interference reaches exactly zero only when K ≥ the interference graph's chromatic number — in practice ~57–83% of sparse runs (N=20, K≥4) and ~0% of dense runs (N≥40 for K≤5). In dense regimes the algorithm *minimizes* interference; it cannot eliminate it. (An earlier version of this repository claimed unconditional zero interference from a single lucky run; that claim was wrong and is retracted.)

**4. Near-centralized quality without a controller.** On identical frozen channel realizations (same user positions, same shadowing draws — so differences are attributable to the allocation alone), the distributed equilibrium is statistically close to centralized heuristics and far above naive schemes. Illustrative 6-run result (seed 42, capacity capped at 160 Mbps by the 8 bps/Hz spectral-efficiency ceiling):

| Scheme | Conflict cost | Mbps / served user | Jain |
|---|---|---|---|
| Distributed (NE) | 3.1e-09 | 120.0 ± 8.9 | 0.81 |
| Centralized greedy | 1.4e-09 | 123.5 ± 5.5 | 0.84 |
| DSATUR | 1.6e-09 | 127.0 ± 5.4 | 0.86 |
| Fixed reuse (i mod K) | 5.2e-08 | 80.0 ± 9.1 | 0.61 |
| Random | 5.7e-07 | 73.9 ± 9.9 | 0.58 |

Regenerate paper-grade numbers with `go run . -runs 200` (topologies and channels are seed-reproducible; asynchronous message races are inherently not, which is part of what is being measured).

## Metrics: what they mean (and what they don't)

- **Gain over Greedy** — the ratio of the distributed solution's conflict cost to a centralized greedy heuristic's. This is **not** the Price of Anarchy: the denominator is a heuristic, not the optimum. An earlier version of this code mislabeled it as PoA; it has been renamed.
- **Empirical PoA lower bound** — the true social optimum is computed exactly by a branch-and-bound solver (`optimum.go`: connected-component decomposition, cost pruning, color-symmetry breaking, time budget). Since one run observes one Nash equilibrium (not the worst one), the ratio NE/OPT is reported as a *lower bound* on PoA, and only when optimality was proven within the time budget.
- **Jain fairness** — *descriptive only*. The algorithm does not optimize fairness; the index reflects the throughput distribution under stochastic user placement. Frozen-channel comparisons (above) are the meaningful way to compare fairness across schemes.

## Repository layout

| File | Contents |
|---|---|
| `types.go` | Types, protocol/PHY constants, scalable protocol timers, message counters |
| `basestation.go` | Agent lifecycle, best response, message handling, contention resolution |
| `physics.go` | Frozen-channel downlink SINR / capped Shannon capacity model |
| `metrics.go` | Jain index, global objective, greedy baseline wrapper |
| `baselines.go` | Greedy, DSATUR, fixed-reuse and random allocators (shared cost definition) |
| `optimum.go` | Exact social optimum via branch-and-bound |
| `experiment.go` | Reproducible topology builder, logical-convergence runner, Monte Carlo core |
| `sweep.go` | K × N convergence sweep with CSV export |
| `main.go` | CLI entry point and single-run (educational) mode |
| `plot_sweep.py` | Convergence CDFs, scaling plots, zero-interference heatmap (matplotlib only) |
| `*_test.go` | Unit tests incl. analytic PHY checks and B&B-vs-exhaustive validation |

## Quick start

```bash
go test ./...                      # run the test suite

go run . -runs 1 -v                # single detailed run with agent logs + viz_data.json
go run .                           # Monte Carlo, 100 runs, mean ± 95% CI + baseline table
go run . -runs 200 -optbudget 30s  # paper-grade main table (~20 min)

# Convergence sweep (10x accelerated timers), then figures:
go run . -sweep -runs 30 -timescale 0.1
python plot_sweep.py sweep_results.csv sweep
```

| Flag | Default | Meaning |
|---|---|---|
| `-runs` | 100 | Monte Carlo runs (1 = detailed single run); in sweep mode: runs per grid cell |
| `-seed` | 42 | Base seed; run *r* uses seed + *r* (reproducible topologies/channels) |
| `-sweep` | false | Run the K × N convergence sweep |
| `-sweepK`, `-sweepN` | `3,4,5,6` / `20,40,60,80` | Sweep grid (comma-separated) |
| `-timescale` | 1.0 | Scales all protocol timers proportionally (0.1 = 10× faster; ratios preserved) |
| `-optbudget` | 3s | Time budget per run for the exact-optimum solver |
| `-csv` | `sweep_results.csv` | Sweep raw-data output (per-run rows; CDFs are plotted from this) |
| `-v` | false | Agent-level logging (single-run mode only) |

## Methodology notes

- **Logical convergence, not wall clock.** A run ends when every station reaches `COMMITTED` (with a safety cap), and convergence time is reported in protocol rounds (think-periods) — a timescale-invariant unit.
- **Frozen channels.** All allocation schemes in a run are evaluated on one channel realization (user positions + all shadowing draws), isolating the allocation effect from placement luck.
- **Physical model.** Downlink SINR at the user position; interference from actually-transmitting (committed) co-channel neighbors over the true interferer→UE geometry; shadowing on serving and interfering links symmetrically; SINR ≤ 30 dB and spectral efficiency ≤ 8 bps/Hz caps.
- **Simulation parameters** (single source of truth in `types.go`): N=40, area 400×400 m, neighbor threshold 100 m, K=5, Ptx=40 W, B=20 MHz, α=3.0, log-normal shadowing.

## Limitations and future work

No theoretical convergence guarantee is claimed for the asynchronous setting (interference games are not always exact potential games); convergence is demonstrated empirically. The topology is static (no mobility, no arrivals/departures). The constant-round finding must be stress-tested beyond N=80 before it can be stated as a scaling law. Fairness is not an optimization target; adding a fairness term to the utility is future work, as is comparing against learning-based (e.g., MARL) allocators.

## Selected references

R. W. Rosenthal, *A class of games possessing pure-strategy Nash equilibria*, Int. J. Game Theory, 1973 · D. Monderer, L. S. Shapley, *Potential games*, GEB, 1996 · P. N. Panagopoulou, P. G. Spirakis, *A game theoretic approach for efficient graph coloring*, ISAAC 2008 · K. Cohen, A. Leshem, E. Zehavi, *Convergence of approximate best-response dynamics in interference games*.
