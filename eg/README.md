# peerkit examples

All examples use the same scenario interface:

```bash
peerkit validate <file>
peerkit expand <file>
peerkit run <file>
```

The current delay model is two-stage:

- `*_delay_distribution` is sampled while the topology is resolved to assign a persistent baseline delay to each node or edge.
- `*_delay_jitter_stddev` adds per-operation/per-transmission truncated-normal jitter around that assigned baseline during execution.

## Compose

| File | Purpose |
|---|---|
| `compose/01-explicit-edge-list.yaml` | Explicit nodes, edges, and per-node/per-edge delay and resource overrides |
| `compose/02-adjacency-matrix.yaml` | Explicit adjacency matrix input |
| `compose/03-domain-er-base.yaml` | Generated ER topology with `base_flooding` |
| `compose/04-domain-grid-duplicate-aware.yaml` | Grid topology with implicit duplicate-neighbor suppression |
| `compose/05-domain-grid-idontwant.yaml` | Grid topology with explicit IDONTWANT control frames |
| `compose/06-scale-er-500.yaml` | Larger single-host configuration; hardware dependent |

## Docker Swarm

| File | Purpose |
|---|---|
| `swarm/01-single-node-local-image.yaml` | One-node Swarm with an image already available locally |
| `swarm/02-multi-node-registry.yaml` | Multi-host Swarm with registry build/push and separate Controller/peer constraints |
| `swarm/03-multi-node-preloaded-image.yaml` | Multi-host Swarm with the same image preloaded on every eligible node |

The `/16` overlay subnets are examples. Change them if they overlap a physical LAN, VPN, or another Docker network.

For Swarm runs, PeerKit waits for peer registration/topology readiness before traffic starts. A completed synchronous run collects the result archive and removes its Swarm services, network, and scenario configs unless the run is intentionally kept.

## Topology generators

`topologies/` contains minimal examples for ER, BA, BA-opportunistic, WS, ring, path, complete, and grid topologies. They are primarily useful with `peerkit validate` and `peerkit expand`.

`ba-opportunistic` keeps BA-style degree-biased attachment, but makes low processing-delay nodes and low-delay candidate links more likely to occupy and grow hub positions. Its delay distributions therefore affect topology construction as well as runtime delay behavior.
