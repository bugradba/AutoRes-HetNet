# AutoRes-HetNet

**Distributed PRB allocation for ultra-dense 5G networks via an asynchronous, message-passing Graph Coloring Game — implemented in Go.**

Each base station is an independent agent (a goroutine) that selects its own frequency block (PRB / "color") by exchanging real messages with its neighbors only. There is no central controller, no shared memory, and no synchronized rounds — agents race, collide, back off and retry, exactly like a real distributed protocol.

## The problem

In an ultra-dense network, nearby base stations sharing the same frequency block interfere with each other and throughput collapses. The classical fix is a centralized controller that sees the whole network and assigns frequencies — but it becomes a computational and latency bottleneck as the network densifies, and a single point of failure.

AutoRes-HetNet models the problem as a non-cooperative **Graph Coloring Game**: nodes = base stations, edges = interference relationships (weighted by path loss + log-normal shadowing), colors = orthogonal PRBs. Each agent repeatedly plays its **best response** (the color minimizing weighted conflict with its neighbors' last known colors) until the system settles into a Nash equilibrium.

## What is actually new here

The cell-as-player, color-as-frequency idea is well established in the potential-game literature. The contribution of this project is the **implementation and its empirical convergence analysis**:

- **Genuinely asynchronous and message-based.** Agents communicate through a 4-message protocol (`HELLO`, `PROPOSE`, `SUCCESS`, `CONFLICT`) over Go channels, with random start delays, message races, timeouts and queue drops — not the synchronous round-based or "one player moves at a time" abstraction most prior work assumes.
- **Contention resolution inside the protocol.** ID-priority plus random backoff (CSMA-like) resolves same-color proposal races. Because interference is a *cost* rather than a hard constraint, a station that is objected to may insist when the contested color is still strictly its cheapest option — this prevents the objection/backoff livelock that a hard veto would create in a weighted game.
- **Commitment is revisable.** `COMMITTED` stations keep re-evaluating their best response against their current neighbor view and re-enter the proposal game whenever they can strictly improve. Convergence is declared only after a network-wide quiet window, and the resulting allocation is then *audited* for the Nash property against ground-truth colors.
- **A genuinely heterogeneous network (A3).** The network is a mix of macro cells (46 dBm, 25 m) and pico cells (30 dBm, 10 m), not a homogeneous field of identical stations — so the "HetNet" in the name is real. Every physical quantity (path loss, breakpoint, SINR, and the coupling weight) is computed per-station from its own power and height.
- **Interference is not truncated at the game graph (A2).** Interference at a user is summed over *all* co-channel stations within a physical radius, not only the game-graph neighbors inside the 100 m messaging threshold. Ignoring out-of-neighborhood co-channel interferers (the old behavior) inflated throughput by nearly 2× — a concrete demonstration of why the physical layer must be taken seriously.
- **End-to-end physical layer on a standard channel model.** The color assignment is carried through a **3GPP TR 38.901 Urban Macro** channel (distance-dependent LOS probability, LOS/NLOS path loss with breakpoint, LOS 4 dB / NLOS 6 dB shadowing) to per-user downlink SINR, capped Shannon capacity, cell-edge (5th-percentile) rate and Jain fairness — not just "conflict counts". The path-loss, LOS-probability and noise equations are unit-tested against the standard's closed forms.

## Key empirical findings

All numbers below come from seeded Monte Carlo experiments with 95% confidence intervals (mean ± 1.96·σ/√n), not single runs. Every number in this section was produced at the current HEAD by the command shown next to it.

**1. The final allocation is a *verified* Nash equilibrium.** After each run, an independent audit (`VerifyNashEquilibrium`) recomputes every station's best response against the *true* final colors of its neighbors — not the agent's possibly-stale local view. Across `go run . -runs 10` and the sweep below, **every converged run ended with 0 deviation wishes**. This is possible because `COMMITTED` is no longer a terminal state: committed stations keep re-evaluating their best response each think-period and re-enter the proposal game whenever they can strictly improve, and convergence is only declared after a full quiet window with no state/color changes anywhere. (Earlier versions logged "NASH EQUILIBRIUM REACHED" per station without any such mechanism or audit; in 8/8 audited runs the old final states were *not* equilibria. That claim was wrong for the old code and holds only now.)

**2. Convergence takes ~10–17 protocol rounds and grows mildly with density.** From `go run . -sweep -runs 5 -sweepK "3,5" -sweepN "20,40" -timescale 0.1`: ~10–12 think-periods at N=20 versus ~15–17 at N=40 (fixed 400×400 m area), with a 100% convergence rate in every tested cell. Rounds are think-periods, so results are `-timescale`-invariant. (An earlier README claimed density-independent ~8-round convergence; that number predates equilibrium verification, could not be reproduced, and is retracted. Post-commit re-evaluation necessarily costs extra rounds — that is the price of an actual equilibrium.)

**3. Message complexity stays modest, and nothing is silently lost anymore.** Messages per station grew ~9 → ~22 as N went 20 → 40 at fixed area (average degree roughly doubles). `CONFLICT` traffic grows much faster than total traffic (2 → 132 per 5 runs at K=3). Queue drops are now *counted* per station; 0 drops were observed in all tested runs.

**4. Zero interference is a property of the graph, not of the algorithm.** Interference reaches exactly zero only when K ≥ the interference graph's chromatic number: measured 80% of runs at N=20, K=5, and 0% in the tested N=40 cells (K=3 and K=5). In dense regimes the algorithm *minimizes* interference; it cannot eliminate it. (An earlier version of this repository claimed unconditional zero interference from a single lucky run; that claim was wrong and is retracted.)

**5. Cell-edge users are where allocation actually matters.** Under TR 38.901 UMa most cell-centre users saturate the 7.4 bps/Hz ceiling regardless of allocation, so *mean* throughput is a blunt instrument: distributed, greedy and DSATUR sit within a few Mbps of each other. The 5th-percentile (cell-edge) rate separates them sharply — and separates all of them from the naive schemes by more than an order of magnitude, because under fixed reuse and random allocation the worst-served users are effectively in outage (sub-1 Mbps). Cell-edge rate, not mean rate, is the metric to lead with.

**6. Near-centralized quality without a controller.** On identical frozen channel realizations (same user positions, same shadowing draws — so differences are attributable to the allocation alone), the distributed equilibrium is statistically indistinguishable from centralized heuristics and far above naive schemes. Measured with `go run . -runs 10` (seed 42; capacity capped at 160 Mbps by the 8 bps/Hz spectral-efficiency ceiling):

| Scheme | Conflict cost (W) | Mbps / served user | Jain |
|---|---|---|---|
| Distributed (NE) | 1.55e-09 ± 4.0e-10 | 121.1 ± 8.9 | 0.81 |
| Centralized greedy | 1.49e-09 ± 3.9e-10 | 119.8 ± 7.2 | 0.80 |
| DSATUR | 1.45e-09 ± 4.8e-10 | 122.7 ± 7.9 | 0.82 |
| Fixed reuse (i mod K) | 4.56e-08 ± 2.1e-08 | 74.6 ± 4.3 | 0.57 |
| Random | 4.06e-07 ± 5.9e-07 | 69.3 ± 5.3 | 0.53 |

**7. Ablation: what each mechanism actually buys.** The two protocol mechanisms can be switched off independently (`-ablate-idpriority`, `-ablate-recheck`), reconstructing the original (pre-fix) protocol from current HEAD. 10 paired runs (seed 42, identical topologies/channels across arms):

| ID-priority | Post-commit re-evaluation | Interference (W) | NE-verified runs | Rounds |
|---|---|---|---|---|
| ✗ | ✗ *(original protocol)* | 4.3e-09 ± 2.6e-09 | 0/10 | 8.0 |
| ✓ | ✗ *(H-1 fix only)* | 2.3e-09 ± 5.8e-10 | 0/10 | 8.1 |
| ✗ | ✓ | 1.7e-09 ± 4.2e-10 | 10/10 | 14.3 |
| ✓ | ✓ *(current)* | 1.6e-09 ± 4.2e-10 | 10/10 | 15.9 |

Three take-aways: (i) contention resolution alone roughly halves residual interference but **cannot** deliver equilibria (0/10 NE without re-evaluation); (ii) re-evaluation is what turns the protocol into an equilibrium-finding dynamic, at the price of ~2× more rounds; (iii) the retracted "constant ~8-round convergence" of earlier versions is exactly reproduced by the ablated arms — it was an artifact of declaring convergence at terminal commitment instead of verifying equilibrium.

Regenerate paper-grade numbers with `go run . -runs 200` (topologies and channels are seed-reproducible; asynchronous message races are inherently not, which is part of what is being measured).

**8. The cost function was mispricing interference — and fixing it doubled cell-edge throughput.** The game's edge weights were originally a *geometric proxy*: coupling strength inferred from BS↔BS distance. But interference is felt at the *user*, and the two diverge systematically. A weak edge means "the base stations are far apart" — which is exactly the regime where BS↔BS distance says least about how close the interferer sits to the victim's user. Since the game pushes collisions onto the weakest edges by construction, it was systematically colliding precisely where its own cost model was least reliable. In one measured topology the weakest edge in the entire graph (essentially free, in the game's view) connected two stations whose interferer sat **8.4 m from the victim's user in line-of-sight**.

The fix is to define the weight as the physical two-way interference power under the same frozen 38.901 channel the evaluation uses:

```
w_ij = Ptx · [ G(j → UE_i) + G(i → UE_j) ]
```

This is symmetric by construction, so the exact-potential structure (and with it the convergence argument) survives. Two properties make it more than a re-parameterisation, and both are unit-tested: the potential function now equals *exactly* the network's total co-channel interference power, and each station's cost includes the interference it **causes** as well as the interference it **suffers** — the externality is internalised, which is precisely why the social cost is an exact potential.

Ablation over 60 paired runs (`-coupling geometric` vs `-coupling physical`, identical seeds, topologies and channel realisations — the allocation-blind baselines are byte-identical across arms, which verifies the pairing):

| Distributed (NE) | Geometric proxy | Physical coupling |
|---|---|---|
| Mean user rate | 125.8 ± 2.3 Mbps | 131.1 ± 2.3 Mbps |
| Cell-edge (5th pct) | 18.4 ± 7.4 Mbps | **43.1 ± 10.9 Mbps** |
| Jain | 0.876 ± 0.014 | 0.912 ± 0.013 |

Cell-edge rate more than doubles at zero protocol cost — same messages, same convergence behaviour, only a better-posed objective. Every weight-aware scheme benefits (centralized greedy and DSATUR improve too, since they consume the same weights); fixed reuse and random are unchanged, as they ignore weights entirely. DSATUR still leads on throughput despite the distributed solution now achieving *lower* weighted cost, which is itself informative: minimising summed interference is not the same as maximising capped log-rate, and a scheme that spreads interference evenly can beat one that minimises its total while concentrating it on a few victims.

**9. Price of Anarchy is reliable at small N — and the distributed equilibrium is nearly optimal there.** The branch-and-bound solver is seeded with a greedy incumbent, so every returned bound is at least as good as the centralized greedy solution (a unit test enforces this; previously an unseeded search could burn its whole budget and return a bound 100× worse than greedy). But the deeper limit is structural: at N=40 the interference graph is almost always a *single* ~40-node connected component, so component decomposition buys nothing and exact proof rarely fits the time budget (this is NP-hard, not a solver defect). PoA is therefore reported honestly as a function of N. At N=20 the solver proves the optimum in **40/40 runs**, giving a trustworthy PoA of **1.10 ± 0.18** (max 4.41) — the distributed equilibrium is within ~10% of the social optimum on average, and exactly optimal in most runs. At N=40 PoA is reported only over the runs where the optimum was proven, with an explicit note that those runs are biased toward easier (sparser) instances. Use `-N 20` for the paper's PoA table; the six runs where OPT=0 but NE>0 (PoA undefined) are excluded from the mean and counted separately.

**10. Truncating interference at the game graph inflates throughput by ~2×.** The old model summed interference only over game-graph neighbors (within the 100 m messaging threshold), silently treating co-channel stations beyond it as zero. Under the full physical model (A2, interference over all co-channel stations within 600 m), mean throughput drops from ~95 Mbps to ~48 Mbps at N=40 — the earlier figures were optimistically biased. This is the single most concrete argument for taking the PHY layer seriously: the convenient locality of the game graph is not a licence to localize the physics.

**11. HetNet trades interference for coverage, not for free capacity.** Introducing pico cells (A3, half the stations at 1 W/10 m) cuts the total conflict cost by ~5.5× — pico stations radiate 16 dB less power, so they generate far less interference. But mean user rate barely moves (~48 → ~45 Mbps), because a pico's *own* users also receive a weaker signal: less interference caused, weaker service delivered, and the two roughly cancel. This is the honest HetNet story — pico cells are a densification/coverage tool, not a mean-throughput multiplier — and it is exactly the kind of nuance a homogeneous-network model cannot express.

**12. Paired analysis: the comparison the design actually licenses.** All schemes are evaluated within the same run, on the same topology and the same frozen channel, so per-run *differences* are the statistically correct unit — topology luck, which dominates the variance, cancels out. The Monte Carlo summary therefore reports paired differences (mean, median, 95% CI, paired t-test, win counts) alongside the marginal table. This matters: independent confidence intervals on the marginals overlap heavily and suggest "indistinguishable", while the paired test resolves the ordering. Both the mean and the median difference are reported, because a mean difference driven by a handful of outlier runs tells a different story than a consistent per-run advantage — and with 60 runs the two comparisons closest to zero (vs. greedy, vs. DSATUR) can flip between marginal significance and non-significance across independent replications. Report the paired table at `-runs 200`, and read the median and win count next to the mean rather than the p-value alone.

## Metrics: what they mean (and what they don't)

- **Gain over Greedy** — the ratio of the distributed solution's conflict cost to a centralized greedy heuristic's. This is **not** the Price of Anarchy: the denominator is a heuristic, not the optimum. An earlier version of this code mislabeled it as PoA; it has been renamed.
- **Empirical PoA lower bound** — the true social optimum is computed exactly by a branch-and-bound solver (`optimum.go`: connected-component decomposition, cost pruning, color-symmetry breaking, time budget). Since one run observes one Nash equilibrium (not the worst one), the ratio NE/OPT is reported as a *lower bound* on PoA, and only when optimality was proven within the time budget.
- **Jain fairness** — *descriptive only*. The algorithm does not optimize fairness; the index reflects the throughput distribution under stochastic user placement. Frozen-channel comparisons (above) are the meaningful way to compare fairness across schemes.

## Repository layout

| File | Contents |
|---|---|
| `types.go` | Types, protocol/PHY constants, scalable protocol timers, message counters |
| `basestation.go` | Agent lifecycle, best response, message handling, contention resolution |
| `physics.go` | 3GPP TR 38.901 UMa channel: LOS probability, LOS/NLOS path loss, frozen-channel SINR and capped Shannon capacity |
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
| `-pico-fraction` | 0.5 | HetNet: fraction of stations that are pico (1 W/10 m); 0 = homogeneous macro network |
| `-interf-radius` | 600 | Physical interference radius (m); interference is summed over all co-channel stations within it, independent of the game threshold |
| `-N` | 40 | Station count. Small N (e.g. 20) lets B&B prove the optimum every run ⇒ reliable PoA; N=40 is the headline density but yields a single giant component |
| `-coupling` | physical | Game cost function: `physical` (two-way interference power under the frozen channel) or `geometric` (legacy BS↔BS distance proxy, for ablation) |
| `-ablate-idpriority` | false | Ablation: disable WAITING-WAITING ID-priority objections (reproduces the effective behavior of the historical H-1 bug) |
| `-ablate-recheck` | false | Ablation: make `COMMITTED` terminal again (pre-re-evaluation protocol) |

## Methodology notes

- **Logical convergence, not wall clock.** A run ends when every station is `COMMITTED` *and* no station changed state or color for a full quiet window (with a safety cap) — an instantaneous all-committed snapshot is not sufficient once commitments are revisable. Convergence time is the moment of the last observed change, reported in protocol rounds (think-periods) — a timescale-invariant unit. Every converged run is additionally audited for the Nash property.
- **Frozen channels.** All allocation schemes in a run are evaluated on one channel realization (user positions + all shadowing draws), isolating the allocation effect from placement luck.
- **Physical model — 3GPP TR 38.901 UMa.** fc = 3.5 GHz, hBS = 25 m, hUT = 1.5 m, Ptx = 46 dBm, B = 20 MHz. Each link's LOS state is drawn from the standard's distance-dependent LOS probability (Table 7.4.2-1); path loss follows Table 7.4.1-1 (LOS two-slope with breakpoint d'BP; NLOS as max(PL_LOS, PL'_NLOS)); shadowing is log-normal with σ = 4 dB (LOS) / 6 dB (NLOS). Noise is N = −174 + 10·log10(B) + NF dBm with NF = 7 dB. Downlink SINR is evaluated at the user position, with interference from actually-transmitting (committed) co-channel neighbors over the true interferer→UE geometry. Caps: SINR ≤ 30 dB, spectral efficiency ≤ 7.4 bps/Hz (5G NR 256-QAM practical limit ⇒ 148 Mbps at 20 MHz).
- **Heterogeneous stations (A3).** Each station is macro (40 W, 25 m) or pico (1 W, 10 m); the pico fraction is set by `-pico-fraction` (default 0.5, 0 recovers a homogeneous macro network). The per-station power and height feed the path loss and SINR. The coupling weight generalizes to w_ij = P_j·G(j→UE_i) + P_i·G(i→UE_j), which stays symmetric (swapping i↔j swaps the two terms) so the exact-potential structure survives — a unit test enforces this under a HetNet.
- **Full interference, not neighborhood-truncated (A2).** The SINR denominator sums interference from every co-channel station within `-interf-radius` (default 600 m ≈ whole network), decoupled from the 100 m game threshold. A unit test asserts that total physical interference strictly exceeds the neighborhood-only sum.
- **Game cost is physically grounded (`-coupling physical`, default).** Edge weights are the two-way interference power `w_ij = Ptx·[G(j→UE_i) + G(i→UE_j)]` under the same frozen channel used for evaluation, so the potential function equals the network's total co-channel interference power and each station internalises the interference it causes. Symmetry — the exact-potential condition — is preserved by construction. The historical geometric proxy (BS↔BS distance) is retained as `-coupling geometric` for ablation. Edge weights still never enter the SINR computation itself, which uses only interferer→UE geometry; a unit test asserts that perturbing a weight by 1000× leaves throughput unchanged.
- **Simulation parameters** (single source of truth in `types.go`): N=40, area 400×400 m, neighbor threshold 100 m, K=5, Ptx=40 W (46 dBm), B=20 MHz, UE distance 10–150 m. Channel: TR 38.901 UMa as above.

## Limitations and future work

With symmetric edge weights this coloring game is an exact potential game, so *sequential* better-response dynamics provably terminate; no such guarantee is claimed for the asynchronous message-passing dynamic actually implemented here (simultaneous moves, stale views, message races), where convergence is demonstrated empirically — 100% of tested runs, each ending in an audited equilibrium. The topology is static (no mobility, no arrivals/departures). The constant-round finding must be stress-tested beyond N=80 before it can be stated as a scaling law. Fairness is not an optimization target; adding a fairness term to the utility is future work, as is comparing against learning-based (e.g., MARL) allocators.

## Selected references

R. W. Rosenthal, *A class of games possessing pure-strategy Nash equilibria*, Int. J. Game Theory, 1973 · D. Monderer, L. S. Shapley, *Potential games*, GEB, 1996 · P. N. Panagopoulou, P. G. Spirakis, *A game theoretic approach for efficient graph coloring*, ISAAC 2008 · K. Cohen, A. Leshem, E. Zehavi, *Convergence of approximate best-response dynamics in interference games*.
