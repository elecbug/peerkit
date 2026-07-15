# peerkit

`peerkit` is a static-topology P2P propagation experiment tool built on go-libp2p and Docker Compose.

It creates one container per peer, restricts libp2p connections to the configured graph, applies node and edge performance models, injects traffic, and produces JSONL/CSV experiment results.

## Current scope

- Single Docker host
- Static, undirected topology
- Explicit edge-list or adjacency-matrix input
- Generated ER, BA, WS, ring, path, complete, and grid domains
- Deterministic generation from `experiment.seed`
- Fixed or random per-message traffic sources
- Three flooding protocols
- Node processing delay, workers, and queues
- Edge delay, loss, bandwidth, and queues
- Per-message propagation and control-plane metrics

## Requirements

- Go 1.23 or later
- Docker Engine or Docker Desktop
- Docker Compose v2

## Quick start

```bash
go run ./cmd/peerkit validate examples/er-domain.yaml
go run ./cmd/peerkit expand -o resolved.yaml examples/er-domain.yaml
go run ./cmd/peerkit run examples/er-domain.yaml
```

Use an existing image:

```bash
docker build -t peerkit-peer:dev -f deploy/Dockerfile .
go run ./cmd/peerkit run --no-build examples/er-domain.yaml
```

## Protocol selection

The top-level `protocol` field selects the forwarding protocol. Omitting it defaults to `base_flooding`.

### `base_flooding`

```yaml
protocol: base_flooding
```

A node forwards the first received copy to every neighbor except the peer that supplied that first copy. Later duplicates are recorded and discarded, but they do not change an already planned forwarding set.

### `duplicate_aware_flooding`

```yaml
protocol: duplicate_aware_flooding
```

A node records every neighbor that sends a duplicate while the first copy is still queued or being processed. At processing completion, those neighbors are excluded from forwarding.

This is an implicit suppression protocol: a duplicate payload must arrive before the sender can be excluded.

### `idontwant_flooding`

```yaml
protocol: idontwant_flooding
```

When a node receives the first copy, it immediately sends an `IDONTWANT(message_id)` control frame to its other neighbors. A peer that receives the control frame suppresses a payload transmission to that neighbor if the payload has not yet been written.

The implementation models the control plane explicitly:

- IDONTWANT frames use the same edge delay, loss rate, and bandwidth model.
- Control frames use a separate prioritized per-edge queue.
- Control and payload random streams are separated so control traffic does not consume payload delay/loss samples.
- A control frame can suppress a payload before enqueue, while queued, or during the emulated edge delay.
- A control frame arriving after the payload write cannot cancel that transmission.

This is a GossipSub-inspired experiment protocol, not a full GossipSub implementation. It does not create topic meshes, IHAVE/IWANT gossip, scoring, or heartbeats.

## Compact domain format

`domain` generates explicit nodes and edges before execution.

```yaml
version: 1
protocol: idontwant_flooding

experiment:
  name: er-domain-demo
  seed: 42
  duration_ms: 12000
  warmup_ms: 1000
  control_base_port: 18080

domain:
  n: 100
  id_prefix: n
  zero_padding: 3

  topology:
    model: er
    p: 0.06
    ensure_connected: true

  node:
    processing_delay: "normal(mean=100ms, stddev=25ms)"
    workers: 2
    queue_capacity: 2048
    overflow_policy: drop_new

  edge:
    delay: "exponential(mean=30ms)"
    loss_rate: 0.005
    bandwidth_mbps: 100
    queue_capacity: 2048

traffic:
  - source: random
    start_at_ms: 0
    count: 100
    interval_ms: 50
    payload_size_bytes: 1024
```

`n` and `node_count` are aliases. Generated identifiers use `id_prefix` and `zero_padding`.

### Domain-level node heterogeneity

For a generated domain, this declaration has hierarchical semantics:

```yaml
node:
  processing_delay: "normal(mean=100ms, stddev=25ms)"
```

For node `i`, peerkit first assigns a permanent node mean:

```text
mu_i ~ max(0, Normal(100 ms, 25^2 ms^2))
```

That node then processes each message using:

```text
processing_delay_i ~ max(0, Normal(mu_i, 25^2 ms^2))
```

Therefore:

- generated nodes receive different permanent means;
- all nodes retain the same 25 ms runtime standard deviation;
- the assignment is deterministic for the same `experiment.seed`;
- `peerkit expand` exposes the assigned mean for every node.

This special interpretation applies to `domain.node.processing_delay` when it is normal. Explicit node definitions and top-level `defaults.node` retain the ordinary behavior of using the same declared distribution wherever it is inherited.

Other domain processing distributions (`constant`, `uniform`, `exponential`, and `pareto`) are copied without permanent per-node parameter sampling.

## Topology generators

### Erdős-Rényi

```yaml
topology:
  model: er
  p: 0.06
  ensure_connected: true
```

Every possible undirected edge is independently sampled with probability `p`. `ensure_connected` adds the minimum number of bridging edges between generated components.

### Barabási-Albert

```yaml
topology:
  model: ba
  m: 3
```

### Watts-Strogatz

```yaml
topology:
  model: ws
  k: 6
  beta: 0.2
```

### Grid

```yaml
topology:
  model: grid
  rows: 13
  columns: 13
```

Other supported models are `ring`, `path`, and `complete`.

## Delay expressions

Supported examples:

```text
constant(100ms)
fixed(100ms)
uniform(10ms, 50ms)
normal(mean=100ms, stddev=20ms)
gaussian(mu=100ms, sigma=20ms)
exponential(mean=25ms)
pareto(xm=20ms, alpha=2.5)
```

Bare values such as `100`, `100ms`, and `1.5s` are interpreted as constants. Unitless values use milliseconds.

## Explicit topology

```yaml
version: 1
protocol: base_flooding

defaults:
  node:
    processing_delay: "normal(100ms, 20ms)"
    workers: 2
    queue_capacity: 1024
    overflow_policy: drop_new
  edge:
    delay: "exponential(50ms)"
    loss_rate: 0.01
    bandwidth_mbps: 10
    queue_capacity: 1024

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

An adjacency matrix may replace `edges`; matrix edges inherit `defaults.edge`.

## Random traffic sources

```yaml
traffic:
  - source: random
    start_at_ms: 0
    count: 100
    interval_ms: 50
    payload_size_bytes: 1024
```

A source is sampled independently and uniformly for each message. The sequence is deterministic for the same seed and is written to `traffic-plan.csv`.

## Output

```text
.peerkit/runs/<run>/
├── compose.yaml
├── run.yaml
├── scenario.yaml
├── resolved-scenario.yaml
├── traffic-plan.csv
├── config/
└── results/
    ├── <node>.jsonl
    ├── messages.csv
    └── summary.json
```

`messages.csv` includes:

- `transmissions`: successful payload transmissions
- `duplicates`: duplicate payload receptions
- `drops`: payload drops
- `suppressions`: payload transmissions skipped by DAF or IDONTWANT
- `control_sent`
- `control_received`
- `control_drops`
- `control_bytes_sent`

Important raw events include `message_suppressed`, `control_sent`, `control_received`, and `control_dropped` in addition to the base message events.

## Modeling boundary

Payload and IDONTWANT delay, loss, and bandwidth are emulated before writing to persistent libp2p streams. TCP and libp2p connection establishment are not delayed. The configured payload size is used for serialization-delay modeling, but a dummy payload body is not physically transmitted.
