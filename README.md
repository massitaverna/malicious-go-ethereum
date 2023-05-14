# Malicious Go Ethereum

This repository contains the implementation of the attacks
described in the paper "Snapping Snap Sync: Practical Attacks on Go Ethereum Synchronising Nodes"
by Massimiliano Taverna and Kenneth G. Paterson, from ETH Zurich.

## Branches

The project is a fork of the official Go Ethereum repository.

The following branches implement the attacks in the paper:
* `ghost-snap`: Implementation of the Ghost-SNaP attack
* `snap`: Implementation of the SNaP attack
* `ghost128`: Implementation of the Ghost-128 attack

## Design overview
The software design involves three main components.
An attack orchestrator (`attack/orchestrator`) moves forward the attacks through their multiple phases.
A chain builder (`attack/buildchain`) receives commands from the orchestrator and build the malicious chains necessary for the attacks.
A bridge (`attack/bridge`) allows for communications between the orchestrator and the main Go Ethereum software, enabling malicious node behaviour.

## No-responsibility disclaimer
The code here provided is intended for research purposes only.
We do not assume any responsibility for any harm you might cause
using this software.
