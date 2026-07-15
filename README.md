# peerkit

`peerkit` is a static-topology P2P propagation experiment tool built on go-libp2p and Docker Compose.

It creates one Docker container per peer, restricts libp2p connections to the configured graph, propagates messages with eager-push flooding, and records JSONL/CSV metrics. Node processing performance and per-edge delay, loss, bandwidth, and queue capacity are configurable.

## Current scope

- Single Docker host
- Static, undirected topology
- Edge-list or adjacency-matrix input
- One libp2p host per container
- Strict neighbor allow-list through `ConnectionGater`
- Eager-push flooding with duplicate suppression
- Node processing delay and worker pool
- Per-directed-edge transmission queue
- Application-level propagation delay, loss, and bandwidth emulation
- Docker CPU and memory limits
- Per-message result aggregation

Dynamic topology, churn, GossipSub, Kademlia, mDNS, NAT traversal, and multi-host deployment are intentionally excluded from v0.1.

## Requirements

- Go 1.23 or later
- Docker Engine or Docker Desktop
- Docker Compose v2 (`docker compose`)

## Quick start

```bash
go mod tidy
go run ./cmd/peerkit validate examples/ring.yaml
go run ./cmd/peerkit run examples/ring.yaml
```

The `run` command:

1. validates the scenario;
2. generates libp2p identities and per-peer runtime configuration;
3. builds the `peerkit-peer:dev` image;
4. starts one container per node;
5. forms and verifies the requested topology;
6. injects the configured traffic;
7. writes raw events and aggregate results;
8. removes the containers unless `--keep` is set.

Use an already-built image:

```bash
docker build -t peerkit-peer:dev -f deploy/Dockerfile .
go run ./cmd/peerkit run --no-build examples/ring.yaml
```

Keep the stopped containers for inspection after the experiment:

```bash
go run ./cmd/peerkit run --keep examples/ring.yaml
```

Stop a retained run:

```bash
go run ./cmd/peerkit down .peerkit/runs/<run-directory>
```

## Scenario format

### Topology by edge list

```yaml
topology:
  directed: false
  nodes:
    - id: n0
    - id: n1
    - id: n2
  edges:
    - source: n0
      target: n1
    - source: n1
      target: n2
```

### Topology by adjacency matrix

```yaml
topology:
  directed: false
  nodes:
    - id: n0
    - id: n1
    - id: n2
  matrix:
    - [0, 1, 0]
    - [1, 0, 1]
    - [0, 1, 0]
```

A matrix must be square, binary, symmetric, and have a zero diagonal. Matrix edges inherit `defaults.edge` because a matrix cannot carry per-edge attributes.

### Node performance

```yaml
defaults:
  node:
    processing_delay:
      distribution: normal
      mean_ms: 100
      stddev_ms: 20
    workers: 2
    queue_capacity: 1024
    overflow_policy: drop_new
```

Node-level overrides are specified under a node:

```yaml
- id: n1
  performance:
    processing_delay:
      distribution: constant
      value_ms: 300
    workers: 1
    queue_capacity: 256
    overflow_policy: drop_new
  resources:
    cpu_limit: 0.5
    memory_limit_mb: 256
```

### Edge performance

```yaml
defaults:
  edge:
    delay:
      distribution: exponential
      mean_ms: 50
    loss_rate: 0.01
    bandwidth_mbps: 10
    queue_capacity: 1024
```

Per-edge overrides:

```yaml
- source: n1
  target: n2
  network:
    delay:
      distribution: constant
      value_ms: 500
    loss_rate: 0.02
    bandwidth_mbps: 2
    queue_capacity: 128
```

Supported delay distributions:

- `constant`: `value_ms`
- `uniform`: `min_ms`, `max_ms`
- `normal`: `mean_ms`, `stddev_ms`; negative samples are clamped to zero
- `exponential`: `mean_ms`
- `pareto`: `scale_ms`, `shape`

`bandwidth_mbps: 0` disables serialization-delay emulation. Bandwidth is modeled from `payload_size_bytes`; the dummy payload itself is not transmitted over libp2p in v0.1.

### Traffic

```yaml
traffic:
  - source: n0
    start_at_ms: 0
    count: 100
    interval_ms: 100
    payload_size_bytes: 1024
```

All emission times must fall within `experiment.duration_ms`. The experiment may still terminate while messages are in transit if the configured duration is too short.

## Output

Each run creates:

```text
.peerkit/runs/<run>/
├── compose.yaml
├── run.yaml
├── scenario.yaml
├── config/
│   └── <node>.yaml
└── results/
    ├── <node>.jsonl
    ├── messages.csv
    └── summary.json
```

Important raw event types:

- `peer_started`
- `connection_established`
- `message_created`
- `message_received`
- `message_processed`
- `message_sent`
- `message_dropped`

`summary.json` reports average reachability, average completion delay, transmissions, duplicates, and drops.

## Modeling boundary

Edge delay, loss, and bandwidth are applied before writing a message to a persistent libp2p stream. They do not delay the TCP/libp2p handshake or background protocol traffic. This provides deterministic, edge-specific experiment control, but it is not a packet-level network emulator. A future `netem` backend can be added without changing the scenario model.
