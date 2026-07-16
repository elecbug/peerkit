# peerkit

`peerkit` is a static-topology libp2p propagation experiment tool. One logical peer is always one Docker container.

It supports two deployment backends behind the same CLI:

- **Compose:** one Docker host, one service per peer.
- **Swarm:** multiple Docker hosts, one replicated peer service whose tasks are individual peers.

The same scenario model, propagation protocols, traffic definitions, and result schema are used in both modes.

## Features

- Explicit edge-list and adjacency-matrix topologies
- Generated ER, BA, WS, ring, path, complete, and grid topologies
- `base_flooding`, `duplicate_aware_flooding`, and `idontwant_flooding`
- Fixed or random message sources
- Node processing-delay distributions, worker counts, and processing queues
- Edge delay, loss, bandwidth, and bounded dynamic queues
- Per-message and per-control-frame metrics
- Single-host Docker Compose execution
- Multi-host Docker Swarm execution
- Automatic Swarm registration and runtime neighbor configuration
- Detached Swarm runs with integrated status, logs, collection, diagnostics, and cleanup

## Requirements

- Go 1.23 or later
- Docker Engine
- Docker Compose v2 for Compose mode
- A Docker Swarm manager for Swarm mode
- A registry reachable by every Swarm node unless the image is preloaded on every node
- Synchronized clocks on all hosts for cross-host completion-delay measurements

## Build the CLI

The Makefile is only a developer/bootstrap interface. All experiment operations use the `peerkit` binary.

```bash
make build
./bin/peerkit version
```

Optional installation:

```bash
make install
peerkit version
```

The rest of this document assumes `peerkit` is on `PATH`. Replace it with `./bin/peerkit` when using the local binary.

## Unified CLI

```text
peerkit validate <scenario.yaml>
peerkit expand [-o resolved.yaml] <scenario.yaml>
peerkit run [options] <scenario.yaml>
peerkit status [options] <run-directory>
peerkit collect [options] <run-directory>
peerkit logs [options] <run-directory>
peerkit stop <run-directory>
peerkit diagnose <run-directory>
peerkit doctor [--mode compose|swarm]
peerkit image build [--tag image]
peerkit image push [--tag image]
peerkit examples
```

No experiment workflow requires `go run`, a Make target, or a separate result-collection shell script.

## Quick start: Compose

```bash
peerkit doctor --mode compose
peerkit validate examples/compose/03-domain-er-base.yaml
peerkit run examples/compose/03-domain-er-base.yaml
```

A normal synchronous `run` performs the entire lifecycle:

```text
load scenario
→ expand generated domain
→ build the runtime image
→ generate Compose files
→ start peer containers
→ form and verify the topology
→ execute traffic
→ finalize and aggregate results
→ stop/remove the deployment
```

Reuse an existing image:

```bash
peerkit image build --tag peerkit-peer:dev
peerkit run --no-build --image peerkit-peer:dev \
  examples/compose/03-domain-er-base.yaml
```

## Quick start: single-node Swarm

Initialize Swarm if necessary:

```bash
docker swarm init
peerkit doctor --mode swarm
```

Build an image available on the only node:

```bash
peerkit image build --tag peerkit-peer:dev
```

Run:

```bash
peerkit run --no-build --image peerkit-peer:dev \
  examples/swarm/01-single-node-local-image.yaml
```

## Multi-host Swarm

Label nodes that may host peer tasks:

```bash
docker node update --label-add peerkit=true worker-01
docker node update --label-add peerkit=true worker-02
docker node update --label-add peerkit=true worker-03
```

Use a registry-qualified image:

```bash
peerkit run \
  --image registry.example.com/k-p2plab/peerkit:0.7.0 \
  examples/swarm/02-multi-node-registry.yaml
```

The Swarm backend creates:

```text
<stack>_controller   replicas: 1
<stack>_peers       replicas: 0 → N in bounded batches
```

Each peer task is exactly one container and one logical peer. Peer identities are generated at runtime and may change between experiments.

### Separate Controller and peer placement

```yaml
deployment:
  mode: swarm
  swarm:
    controller_constraints:
      - node.role == manager
    peer_constraints:
      - node.labels.peerkit == true
```

The legacy `placement_constraints` field is still accepted as a shared constraint for both services, but it cannot be combined with the separate fields.

### Overlay network

```yaml
deployment:
  mode: swarm
  swarm:
    network:
      subnet: 10.200.0.0/16
      gateway: 10.200.0.1   # optional
      attachable: true
```

The subnet is optional. When omitted, Docker chooses it. A configured subnet must not overlap a physical LAN, VPN, `docker_gwbridge`, ingress network, or another Docker network.

When a subnet is configured, peer tasks explicitly select an interface address inside that CIDR when registering with the Controller. This avoids registering the wrong container interface after changing overlay address ranges.

## Detached Swarm workflow

A detached run returns after deployment and staged scaling. The Controller continues the experiment inside Swarm.

```bash
peerkit run --detach \
  --image registry.example.com/k-p2plab/peerkit:0.7.0 \
  examples/swarm/02-multi-node-registry.yaml
```

The command prints the run directory. Use it for all later operations:

```bash
RUN=.peerkit/runs/<run-name>

peerkit status "$RUN"
peerkit logs --service controller --follow "$RUN"
peerkit collect "$RUN"
```

`collect` performs:

```text
wait for Controller state=completed
→ download peerkit-results.tar.gz
→ validate the archive
→ extract results
→ verify summary.json and messages.csv
→ remove Swarm services
→ remove stack overlay networks
```

Override the Controller URL when the metadata address is not reachable from the current shell:

```bash
peerkit collect \
  --controller-url http://192.168.10.20:18080 \
  "$RUN"
```

Keep the deployment after collection:

```bash
peerkit collect --keep "$RUN"
```

Remove an abandoned run without collecting:

```bash
peerkit stop "$RUN"
```

## Run management

### Status

```bash
peerkit status "$RUN"
peerkit status --json "$RUN"
peerkit status --controller-url http://127.0.0.1:18080 "$RUN"
```

### Logs

```bash
# All available logs (default)
peerkit logs "$RUN"

# Swarm services
peerkit logs --service controller "$RUN"
peerkit logs --service peers --tail 200 "$RUN"
peerkit logs --service all --follow "$RUN"

# Compose: all peers or one generated peer service
peerkit logs --service peers "$RUN"
peerkit logs --service n000 "$RUN"
```

### Diagnostics

```bash
peerkit diagnose "$RUN"
```

Diagnostics are written under:

```text
<run-directory>/diagnostics/
```

For Swarm this includes Controller logs, stack services, stack tasks, and Controller status when available.

## Examples

List all bundled YAML files:

```bash
peerkit examples
```

See [`examples/README.md`](examples/README.md) for a mode-by-mode index.

Important examples:

| Example | Purpose |
|---|---|
| `examples/compose/01-explicit-edge-list.yaml` | Explicit topology and overrides |
| `examples/compose/02-adjacency-matrix.yaml` | Matrix topology input |
| `examples/compose/03-domain-er-base.yaml` | ER + base flooding |
| `examples/compose/04-domain-grid-duplicate-aware.yaml` | Duplicate-aware flooding |
| `examples/compose/05-domain-grid-idontwant.yaml` | IDONTWANT flooding |
| `examples/compose/06-scale-er-500.yaml` | Large single-host run |
| `examples/swarm/01-single-node-local-image.yaml` | One-node Swarm, local image |
| `examples/swarm/02-multi-node-registry.yaml` | Multi-host Swarm, registry |
| `examples/swarm/03-multi-node-preloaded-image.yaml` | Multi-host Swarm, preloaded image |

## Scenario structure

```yaml
version: 1
protocol: base_flooding

experiment:
  name: example
  seed: 42
  duration_ms: 10000
  warmup_ms: 1000
  control_base_port: 18080

deployment:
  mode: compose

controller:
  parallelism: 32
  operation_timeout_seconds: 180

metrics:
  buffer_bytes: 262144
  queue_capacity: 512
  flush_interval_ms: 200

# Use either domain or topology.
domain: {}
traffic: []
```

## Protocols

### `base_flooding`

A node forwards the first received copy to every neighbor except the peer that supplied that first copy. Later duplicates are recorded and discarded but do not modify the already planned forwarding set.

```yaml
protocol: base_flooding
```

### `duplicate_aware_flooding`

A node records neighbors that send a duplicate while the first copy is queued or processed. At processing completion, those neighbors are removed from the forwarding set.

```yaml
protocol: duplicate_aware_flooding
```

This is implicit suppression: a duplicate payload must arrive before the sender can be excluded.

### `idontwant_flooding`

A node receiving the first copy immediately sends an `IDONTWANT(message_id)` control frame to its other neighbors. A receiver suppresses an unsent payload for the control-frame sender.

```yaml
protocol: idontwant_flooding
```

Control frames use the configured edge delay, loss, bandwidth, and prioritized control queue. This protocol is GossipSub-inspired but does not implement topic meshes, IHAVE/IWANT, peer scoring, or heartbeats.

## Generated domains

```yaml
domain:
  n: 200
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
```

Supported topology models:

| Model | Parameters |
|---|---|
| `er` | `p` or `average_degree`, optional `ensure_connected` |
| `ba` | `m` |
| `ws` | `k`, `beta` |
| `ring` | none |
| `path` | none |
| `complete` | none |
| `grid` | `rows`, `columns` |

For size-comparison experiments, prefer ER `average_degree` over fixed `p`:

```text
p = average_degree / (n - 1)
```

This keeps the expected edge count approximately linear in `n`.

### Node heterogeneity

For a generated domain:

```yaml
processing_delay: "normal(mean=100ms, stddev=25ms)"
```

assigns each node a fixed mean sampled around 100 ms while retaining the same 25 ms runtime standard deviation for every node. Use `peerkit expand` to inspect the resolved per-node means.

## Distribution expressions

```text
constant(100ms)
uniform(10ms, 50ms)
normal(mean=100ms, stddev=25ms)
exponential(mean=30ms)
pareto(scale=20ms, shape=2.5)
```

Durations accept `ms` and `s`. Unitless numeric values are interpreted as milliseconds.

## Traffic

Fixed source:

```yaml
traffic:
  - source: n000
    start_at_ms: 0
    count: 100
    interval_ms: 50
    payload_size_bytes: 1024
```

Random source per message:

```yaml
traffic:
  - source: random
    start_at_ms: 0
    count: 100
    interval_ms: 50
    payload_size_bytes: 1024
```

The generated source plan is written to `traffic-plan.csv`.

## Results

Each run has a self-contained directory:

```text
.peerkit/runs/<run>/
├── run.yaml
├── scenario.yaml
├── resolved-scenario.yaml
├── compose.yaml or stack.yaml
├── peerkit-results.tar.gz       # Swarm collection
├── diagnostics/
└── results/
    ├── n000.jsonl
    ├── n001.jsonl
    ├── ...
    ├── traffic-plan.csv
    ├── messages.csv
    └── summary.json
```

`messages.csv` includes:

- reachability and completion delay
- transmissions and duplicates
- drops and suppressions
- IDONTWANT control sent/received/dropped counts
- control bytes

## Image workflows

Build locally:

```bash
peerkit image build --tag peerkit-peer:dev
```

Push to a registry:

```bash
peerkit image build --tag registry.local:5000/peerkit:0.7.0
peerkit image push --tag registry.local:5000/peerkit:0.7.0
```

A synchronous `peerkit run` builds automatically unless `--no-build` is specified. When Swarm `push_image: true`, the image reference must be registry-qualified and the run command also pushes it.

## Troubleshooting

### Swarm services remain at `0/N`

```bash
peerkit status "$RUN"
peerkit diagnose "$RUN"
```

Common causes:

- placement constraints do not match any active node;
- the image is unavailable on workers;
- resource limits cannot be scheduled;
- a node is paused or drained.

### Controller is reachable but collection fails

Use the same reachable address explicitly:

```bash
peerkit collect --controller-url http://<manager-ip>:18080 "$RUN"
```

### Overlay subnet changes break peer registration

Set `deployment.swarm.network.subnet`. Peer tasks then register only an interface address inside that CIDR.

### Large single-host Compose runs fail during cgroup creation

Reduce `deployment.compose_parallelism`, lower per-peer resource overhead, or use Swarm across multiple hosts. The one-peer-per-container invariant is preserved in both modes.

## Development

```bash
make build
make test
make check
```

Runtime operations should still be performed through `bin/peerkit`; the Makefile does not duplicate the user-facing command surface.
