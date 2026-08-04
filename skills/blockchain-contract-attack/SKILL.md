---
name: blockchain-contract-attack
description: >-
  Blockchain/smart contracts: Etherscan, slither/mythril, reentrancy/access control/oracles/flash loans, cross-chain bridges, and exposed RPC. Use when auditing smart contracts, DeFi, or blockchain attack surfaces.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Blockchain / Smart Contracts

```
=== Blockchain/smart contracts ===
Source: Etherscan getsourcecode API | audit with slither/mythril/manticore
Manual: reentrancy (.call{value} transfers before state changes, violating C-E-I) | access control (missing onlyOwner) | integer overflow (<0.8 without SafeMath)
  Oracle manipulation (flash-loan manipulation of AMM prices) | randomness (controllable block.timestamp) | authorization abuse (unlimited approve/permit replay) | delegatecall proxy storage collision
DeFi: flash-loan attacks/sandwich attacks (front-running)/governance attacks/signature replay | cross-chain bridges: signature-threshold bypass/replay | RPC: exposed 8545 allows direct eth_sendTransaction
```
