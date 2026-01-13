# Autonomous and Distributed Resource Management in 5G/6G HetNets

## Abstract

This study addresses the Physical Resource Block (PRB) allocation problem in 5G heterogeneous networks (HetNets) using a fully distributed and scalable approach. The proposed method eliminates the need for a centralized controller by enabling autonomous base stations to make local resource allocation decisions while collectively optimizing a global objective function.

The system is based on a Graph Coloring Game (GCG) formulation and is evaluated under realistic physical layer conditions, including Urban Macro (UMa) channel models and stochastic user placement. Simulation results conducted with 40 base stations demonstrate that the proposed distributed algorithm completely eliminates inter-cell interference, achieving an average interference level of −56.34 dBm.

Despite relying solely on local information, the algorithm successfully converges to a Nash Equilibrium, reducing the initially high interference cost to zero. This behavior provides strong evidence that local autonomous decision-making can lead to global network coordination.

Under a bandwidth of 20 MHz, the system achieves an average user throughput of 223.72 Mbps, while maintaining a Jain’s Fairness Index of 0.7140. The results confirm that the proposed distributed resource management framework is scalable, interference-aware, and well-suited for dense 5G and future 6G network deployments.

## Key Features

- Fully distributed PRB allocation without centralized control
- Game-theoretic formulation using Graph Coloring Games
- Realistic physical layer modeling (Urban Macro)
- Nash Equilibrium convergence
- High throughput and fairness performance
- Scalable architecture for dense HetNet scenarios

## Technologies

- Programming Language: Go
- Simulation Domain: 5G / 6G Wireless Networks
- Core Concepts: Game Theory, Distributed Systems, Resource Management

## Future Work

The current framework can be extended with learning-based methods such as Reinforcement Learning, Federated Learning, and Online Learning to improve adaptability in dynamic and non-stationary network environments.
