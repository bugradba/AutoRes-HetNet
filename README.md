Autonomous and Distributed Resource Management in 5G/6G HetNets
🔍 Project Overview

This project addresses the Physical Resource Block (PRB) allocation problem in 5G heterogeneous networks (HetNets) using a fully decentralized and scalable approach.
Instead of relying on a centralized controller, the system enables autonomous base stations to make local decisions while still achieving global network optimization.

The proposed solution is based on a Graph Coloring Game (GCG) formulation and is evaluated under realistic physical layer conditions, including Urban Macro (UMa) channel models and stochastic user distribution.

🧠 Key Idea

Local autonomy can lead to global order.

Each base station independently selects PRBs by interacting only with its neighbors.
Despite the absence of centralized coordination, the system converges to a Nash Equilibrium, where inter-cell interference is minimized and overall network performance is optimized.

⚙️ System Model & Methodology

Network Type: 5G / 6G Heterogeneous Networks (HetNets)

Number of Base Stations: 40

Bandwidth: 20 MHz

Channel Model: Urban Macro (UMa)

Resource Allocation Model: Graph Coloring Game

Decision Making: Fully distributed (local information only)

Each base station is modeled as a rational agent that:

Competes for PRBs with neighboring stations

Minimizes experienced interference

Updates its strategy iteratively until equilibrium is reached

📊 Simulation Results
Metric	Result
Interference Level	−56.34 dBm (effectively eliminated)
User Throughput (Average)	223.72 Mbps
Jain’s Fairness Index	0.7140
Convergence	Nash Equilibrium achieved
Scalability	High (no central controller)

🔹 Although decisions are made locally, the algorithm successfully optimizes the global objective function, reducing the initially high interference cost to zero at equilibrium.

✅ Key Contributions

✔ Fully distributed PRB allocation without a central controller

✔ Game-theoretic formulation using Graph Coloring Games

✔ Realistic physical layer modeling

✔ Demonstration that local decision-making ensures global coordination

✔ Scalable and suitable for dense 5G/6G deployments

🚀 Future Extensions (Planned)

This framework is highly compatible with learning-based approaches, including:

Reinforcement Learning (RL) for adaptive PRB selection

Federated Learning (FL) for collaborative model training without raw data sharing

Online Learning for real-time adaptation to dynamic network conditions

These extensions can further improve adaptability, robustness, and performance in non-stationary environments.

🛠 Technologies

Language: Go (simulation core)

Visualization: Python (optional)

Modeling: Game Theory, Wireless Channel Models

📌 Conclusion

This project demonstrates that distributed intelligence can effectively replace centralized control in next-generation wireless networks.
By combining game theory, realistic channel modeling, and autonomous agents, the system achieves high throughput, fairness, and zero interference, making it a strong candidate for future 5G/6G resource management architectures.
