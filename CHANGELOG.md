# Changelog

## 0.8.0

- Added `peerkit inspect [run-directory]` with automatic fallback to the most recent run.
- Added Swarm diagnostics for nodes, services, tasks, overlay networks, IPAM capacity, subnet overlap, configs, Controller HTTP state, and published-port conflicts.
- Added `bin/.peerkit-last-run`, written immediately after runtime generation.
- Added argument-free `peerkit stop`, which reads the recent-run state file.
- Added explicit automatic Swarm cleanup after completion and successful result collection.
- Preserved `--keep` and detached collection workflows.
- Fixed failed-task detection so historical `Rejected`, `Failed`, and `Orphaned` task records are not hidden by a desired-state filter.
