# peerkit

`peerkit` is a static-topology P2P propagation experiment tool built on go-libp2p, Docker Compose, and Docker Swarm.

It preserves one logical peer per container, restricts libp2p connections to the configured graph, applies node and edge performance models, injects traffic, and produces JSONL/CSV experiment results. The same scenario and CLI are used for single-host Compose runs and multi-host Swarm runs.

## Current scope

- Static, undirected topology
- One logical peer per Docker container
- Single-host Docker Compose deployment
- Multi-host Docker Swarm deployment
- Explicit edge-list or adjacency-matrix input
- Generated ER, BA, WS, ring, path, complete, and grid domains
- Fixed or random per-message traffic sources
- Three flooding protocols
- Node processing delay, workers, and queues
- Edge delay, loss, bandwidth, and queues
- Per-message propagation and control-plane metrics
- Average-degree ER generation for scale-stable experiments
- Dynamic per-edge queues without queue-capacity-sized preallocation
- Asynchronous buffered JSONL metrics
- Bounded-parallel controller initialization

## Requirements

- Go 1.23 or later
- Docker Engine
- Docker Compose v2 for `deployment.mode: compose`
- A Docker Swarm manager for `deployment.mode: swarm`
- A registry reachable from every Swarm node, unless the image is preloaded on every node
- Synchronized host clocks, such as chrony or systemd-timesyncd, for cross-host delay measurements

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

## Docker Swarm deployment

The user-level command remains unchanged. Set `deployment.mode: swarm` and run the scenario from a Swarm manager:

```yaml
deployment:
  mode: swarm
  swarm:
    push_image: true
    with_registry_auth: true
    startup_timeout_seconds: 600
    startup_batch_size: 25
    startup_batch_interval_ms: 1000
    placement_constraints:
      - node.labels.peerkit == true
    max_replicas_per_node: 200
```

```bash
go run ./cmd/peerkit run \
  --image registry.example.com/peerkit/peerkit:0.6.0 \
  examples/swarm-domain.yaml
```

The CLI performs the following operations automatically:

```text
load and expand scenario
→ build peerkit image
→ push image to the registry
→ generate stack.yaml
→ docker stack deploy with zero peer replicas
→ scale peer tasks in bounded startup batches
→ wait for peer task registration
→ distribute per-peer runtime configurations
→ form and verify the libp2p topology
→ run traffic
→ collect peer JSONL files through the Controller
→ download the result archive
→ docker stack rm
```

The generated stack has two services:

```text
controller   replicas: 1
peers         replicas: 0 → N in startup batches
```

Each `peers` task is one container and one logical peer. The task uses `{{.Task.Slot}}` only to select its corresponding topology node. It generates a fresh libp2p identity when it starts, registers its Peer ID and overlay address with the Controller, and downloads its resolved neighbor configuration after all tasks have registered.

The resolved scenario is gzip-compressed and split into Docker Config parts below the per-config size limit before deployment. The Controller reconstructs it at startup, so generated or explicit topologies larger than a single Docker Config can still be deployed.

Only the Controller API port is published. Peer control APIs and libp2p transport addresses remain inside the overlay network.

### Swarm image distribution

By default, `push_image: true` builds and pushes the image before stack deployment. The image passed with `--image` must therefore be registry-qualified:

```text
registry.example.com/peerkit/peerkit:0.6.0
registry.local:5000/peerkit/peerkit:0.6.0
username/peerkit:0.6.0
```

For a private registry, log in on the manager before execution. `with_registry_auth: true` forwards the manager's registry credentials to Swarm workers.

When the exact image is already available on every node:

```yaml
deployment:
  mode: swarm
  swarm:
    push_image: false
```

```bash
go run ./cmd/peerkit run --no-build --image peerkit-peer:dev examples/swarm-domain.yaml
```

### Swarm placement

Label eligible nodes:

```bash
docker node update --label-add peerkit=true worker-01
docker node update --label-add peerkit=true worker-02
docker node update --label-add peerkit=true worker-03
```

`placement_constraints` are applied to both the Controller and peer service. `max_replicas_per_node` is applied to the replicated peer service after stack deployment.

Peer tasks start in bounded batches rather than all at once. `startup_batch_size` controls the number of new replicas per scale step, and `startup_batch_interval_ms` inserts a pause after each completed step. This reduces simultaneous cgroup, network-namespace, and overlay endpoint creation on each host.

All peer containers in one replicated service must use identical Docker CPU and memory limits. Logical node processing distributions may still differ per peer. Heterogeneous cgroup limits require separate resource-class services and are rejected by the current Swarm generator.

### Swarm result collection

Worker-local bind mounts are not used. Each peer records to its container filesystem, finalizes the JSONL stream, and exposes it to the Controller over the overlay network. The Controller aggregates all peers and serves a compressed result archive to the CLI.

Peer identities are intentionally ephemeral. If a task is recreated, its Peer ID changes. The generated peer service uses `restart_policy.condition: none`, so a task failure causes the current experiment to fail instead of silently replacing a peer midway through the run.

Physical inter-host latency is added to the configured application-level edge delay. Cross-host completion timestamps also assume synchronized host clocks.

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

controller:
  parallelism: 32
  operation_timeout_seconds: 180

metrics:
  buffer_bytes: 262144
  queue_capacity: 512
  flush_interval_ms: 200

domain:
  n: 100
  id_prefix: n
  zero_padding: 3

  topology:
    model: er
    average_degree: 12
    ensure_connected: true

  node:
    processing_delay: "normal(mean=100ms, stddev=25ms)"
    workers: 2
    queue_capacity: 512
    overflow_policy: drop_new

  edge:
    delay: "exponential(mean=30ms)"
    loss_rate: 0.005
    bandwidth_mbps: 100
    queue_capacity: 64

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

For experiments that change `n`, prefer a fixed expected degree:

```yaml
topology:
  model: er
  average_degree: 12
  ensure_connected: true
```

peerkit converts this to:

```text
p = average_degree / (n - 1)
```

This keeps the expected edge count approximately linear in `n`:

```text
E[|E|] = n * average_degree / 2
```

The original probability form remains available:

```yaml
topology:
  model: er
  p: 0.06
```

`p` and `average_degree` are mutually exclusive. `ensure_connected` adds the minimum number of bridging edges between generated components.

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

## Scalability controls

### Dynamic edge queues

`edge.queue_capacity` remains a strict per-direction bound, but peerkit no longer allocates a channel containing that many `outboundItem` slots for every directed edge. Each edge now uses a dynamically growing FIFO and releases its backing slice when empty.

This changes memory behavior from capacity-driven allocation toward actual queued traffic:

```text
previous: approximately O(directed_edges * queue_capacity)
current:  O(directed_edges + currently_queued_frames)
```

Control frames retain priority over payload frames, and FIFO order is preserved within each queue.

### Buffered metrics

```yaml
metrics:
  buffer_bytes: 262144
  queue_capacity: 512
  flush_interval_ms: 200
```

Events are placed in a bounded per-peer queue and written by one writer goroutine. The JSONL buffer is flushed periodically and when the peer closes, rather than after every event.

These values are **per peer**. Large values multiplied by hundreds of containers can consume substantial memory.

### Parallel controller operations

```yaml
controller:
  parallelism: 32
  operation_timeout_seconds: 180
```

Readiness probes, connection commands, topology checks, and stream preparation use bounded parallelism. `operation_timeout_seconds` applies to the connection, topology convergence, and stream-preparation phases.

A larger value does not increase steady-state simulation resource use; it only gives large deployments more time to converge.

### Recommended large-domain starting point

```yaml
controller:
  parallelism: 32
  operation_timeout_seconds: 180

metrics:
  buffer_bytes: 262144
  queue_capacity: 512
  flush_interval_ms: 200

domain:
  n: 500
  topology:
    model: er
    average_degree: 12
    ensure_connected: true
  edge:
    queue_capacity: 64
```

The one-container-per-peer model is retained. For larger experiments, use `deployment.mode: swarm` to distribute those containers across multiple hosts rather than placing multiple logical peers in one container.

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

Compose run:

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

Swarm run:

```text
.peerkit/runs/<run>/
├── stack.yaml
├── run.yaml
├── scenario.yaml
├── resolved-scenario.yaml
└── results/
    ├── scenario.yaml
    ├── resolved-scenario.yaml
    ├── traffic-plan.csv
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
