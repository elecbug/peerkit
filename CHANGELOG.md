# Changelog

## v0.4.0

- Added top-level protocol selection:
  - `base_flooding`
  - `duplicate_aware_flooding`
  - `idontwant_flooding`
- Added explicit IDONTWANT control frames with prioritized per-edge control queues.
- Applied edge delay, loss, and bandwidth models to IDONTWANT traffic.
- Added queued payload suppression when an IDONTWANT is received before the payload write.
- Added payload suppression and control-plane metrics to JSONL, CSV, and summary output.
- Changed generated-domain normal node processing delays to assign deterministic per-node means while retaining a common runtime standard deviation.
- Preserved topology generation for existing seeds by using an independent RNG stream for node-performance assignment.
- Updated examples, Makefile, README, and Docker build dependency caching.
