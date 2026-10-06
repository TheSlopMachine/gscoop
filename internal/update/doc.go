// Package update implements the Phase 3A update and cleanup engines.
//
// Bucket sync fans out across buckets with a small worker pool while
// each repository pull stays serialized through the gitengine seam.
// App updates resolve manifests (including app@version pins through
// bucket git history) and hand the transaction to the install package:
// downloads run in parallel, installs run strictly sequentially.
// Self-update covers the gscoop binary (semver check, channel gate,
// rename-swap) plus a best-effort fast-forward of the classic scoop
// checkout. Cleanup removes old versions and prunes the cache.
// No emojis.
package update
