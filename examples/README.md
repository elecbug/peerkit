# peerkit examples

All examples use the same user interface:

```bash
peerkit validate <file>
peerkit run <file>
```

## Compose

| File | Purpose |
|---|---|
| `compose/01-explicit-edge-list.yaml` | Explicit nodes, edges, and per-node/per-edge overrides |
| `compose/02-adjacency-matrix.yaml` | Explicit adjacency matrix input |
| `compose/03-domain-er-base.yaml` | Generated ER topology with `base_flooding` |
| `compose/04-domain-grid-duplicate-aware.yaml` | Implicit duplicate-neighbor suppression |
| `compose/05-domain-grid-idontwant.yaml` | Explicit IDONTWANT control frames |
| `compose/06-scale-er-500.yaml` | Large single-host configuration; hardware dependent |

## Docker Swarm

| File | Purpose |
|---|---|
| `swarm/01-single-node-local-image.yaml` | One-node Swarm with an image already available locally |
| `swarm/02-multi-node-registry.yaml` | Multi-host Swarm with registry build/push and separate Controller/peer constraints |
| `swarm/03-multi-node-preloaded-image.yaml` | Multi-host Swarm with the same image preloaded on every node |

The `/16` overlay subnets are examples. Change them if they overlap a physical LAN, VPN, or another Docker network.

## Topology generators

`topologies/` contains minimal ER, BA, WS, ring, path, complete, and grid examples. They are primarily useful with `peerkit validate` and `peerkit expand`.
