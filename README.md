# peerkit

`peerkit` is a static-topology P2P propagation experiment tool built on go-libp2p and Docker Compose.

It creates one Docker container per peer, restricts libp2p connections to the configured graph, propagates messages with eager-push flooding, and records JSONL/CSV metrics. Node processing performance and per-edge delay, loss, bandwidth, and queue capacity are configurable.

## Current scope

- Single Docker host
- Static, undirected topology
- Explicit edge-list or adjacency-matrix input
- Generated topology domain input for large experiments
- ER, BA, WS, ring, path, complete, and grid generators
- Deterministic topology generation from `experiment.seed`
- One libp2p host per container
- Strict neighbor allow-list through `ConnectionGater`
- Eager-push flooding with duplicate suppression
- Node processing delay and worker pool
- Per-directed-edge transmission queue
- Application-level propagation delay, loss, and bandwidth emulation
- Docker CPU and memory limits
- Per-message result aggregation

Dynamic topology, churn, GossipSub, Kademlia, mDNS, NAT traversal, and multi-host deployment are intentionally excluded from v0.2.

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

A generated 100-node ER experiment can be validated and expanded without defining every node:

```bash
go run ./cmd/peerkit validate examples/domain.yml
go run ./cmd/peerkit expand -o /tmp/resolved-domain.yaml examples/domain.yml
go run ./cmd/peerkit run examples/domain.yml
```

The `run` command:

1. validates and expands the scenario;
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

## Compact domain format

`domain` is an alternative to `topology`. The two forms cannot be used together. A domain declaration is expanded into explicit nodes and edges before execution.

```yaml
version: 1

experiment:
  name: er-domain-demo
  seed: 42
  duration_ms: 12000
  warmup_ms: 1000

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
  - source: n000
    start_at_ms: 0
    count: 100
    interval_ms: 50
    payload_size_bytes: 1024
```

`n` is the node count; `node_count` is accepted as an explicit alias. Generated IDs are `id_prefix + zero-padded index`. If `zero_padding` is omitted, peerkit uses the number of digits in `count - 1`. For example, 100 nodes become `n000` through `n099`.

### Supported topology generators

#### Erdős–Rényi

```yaml
topology:
  model: er
  p: 0.05
  ensure_connected: true
```

Every possible undirected edge is sampled independently with probability `p`. When `ensure_connected` is true, peerkit identifies connected components and adds exactly `component_count - 1` bridging edges. The resulting graph is therefore an augmented ER graph rather than a rejection-sampled connected ER graph.

Accepted model aliases: `er`, `erdos-renyi`, `erdos_renyi`, `gnp`.

#### Barabási–Albert

```yaml
topology:
  model: ba
  m: 3
```

Generation starts from a clique of `m + 1` nodes. Every subsequent node attaches to `m` distinct existing nodes using degree-proportional selection.

Accepted aliases: `ba`, `barabasi-albert`, `barabasi_albert`.

#### Watts–Strogatz

```yaml
topology:
  model: ws
  k: 6
  beta: 0.2
```

`k` must be positive, even, and smaller than the node count. `beta` must be between 0 and 1.

Accepted aliases: `ws`, `watts-strogatz`, `watts_strogatz`.

#### Other deterministic forms

```yaml
topology:
  model: ring
```

Supported models are `ring`, `path`, and `complete`.

A grid requires dimensions whose product equals the node count:

```yaml
topology:
  model: grid
  rows: 10
  columns: 10
```

One dimension may be omitted when the node count is exactly divisible by the other.

### Inspecting the generated graph

```bash
go run ./cmd/peerkit expand examples/domain.yml
```

Write the fully resolved explicit scenario to a file:

```bash
go run ./cmd/peerkit expand -o resolved.yaml examples/domain.yml
```

The expanded file contains every generated node, edge, and inherited performance setting. It can be edited and run as a normal explicit scenario.

## Distribution expressions

Delay distributions can use the original mapping form or a compact scalar expression.

```yaml
processing_delay: "normal(mean=100ms, stddev=20ms)"
delay: "exponential(25ms)"
```

Supported expressions:

```text
constant(100ms)
fixed(100ms)
uniform(10ms, 50ms)
normal(100ms, 20ms)
normal(mean=100ms, stddev=20ms)
gaussian(mu=100ms, sigma=20ms)
Normal(μ=100ms, σ=20ms)
exponential(25ms)
exp(mean=25ms)
pareto(scale=20ms, shape=2.5)
pareto(xm=20ms, alpha=2.5)
pareto(xm=20ms, α=2.5)
```

Bare values such as `100`, `100ms`, and `1.5s` are interpreted as constant delays. Bare numbers use milliseconds.

The original mapping remains valid:

```yaml
processing_delay:
  distribution: normal
  mean_ms: 100
  stddev_ms: 20
```

Supported runtime distributions are:

- `constant`: `value_ms`
- `uniform`: `min_ms`, `max_ms`
- `normal`: `mean_ms`, `stddev_ms`; negative samples are clamped to zero
- `exponential`: `mean_ms`
- `pareto`: `scale_ms`, `shape`

The node processing distribution is sampled for each processed message. The edge delay distribution is sampled for each edge transmission. It does not sample one permanent processing speed per node or one permanent delay per edge.

## Explicit scenario format

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
    processing_delay: "normal(100ms, 20ms)"
    workers: 2
    queue_capacity: 1024
    overflow_policy: drop_new
```

Node-level overrides are specified under a node:

```yaml
- id: n1
  performance:
    processing_delay: "constant(300ms)"
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
    delay: "exponential(50ms)"
    loss_rate: 0.01
    bandwidth_mbps: 10
    queue_capacity: 1024
```

Per-edge overrides:

```yaml
- source: n1
  target: n2
  network:
    delay: "constant(500ms)"
    loss_rate: 0.02
    bandwidth_mbps: 2
    queue_capacity: 128
```

`bandwidth_mbps: 0` disables serialization-delay emulation. Bandwidth is modeled from `payload_size_bytes`; the dummy payload itself is not transmitted over libp2p in v0.2.

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
├── resolved-scenario.yaml
├── config/
│   └── <node>.yaml
└── results/
    ├── <node>.jsonl
    ├── messages.csv
    └── summary.json
```

`scenario.yaml` is the original input. `resolved-scenario.yaml` is the normalized explicit topology actually used by the run.

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
