// Package commands implements the Phase 1C read-only Scoop subcommands for
// gscoop: list, info, cat, which, prefix, depends, status, export,
// cache show, and checkup, plus dispatch wiring used by cmd/gscoop.
//
// Each file mirrors one libexec/scoop-<cmd>.ps1 surface. Shared filesystem
// access lives in store.go as minimal local seams; every seam carries a TODO
// naming the owning package (config, state, version, manifest, bucket,
// search) that replaces it in later phases. No emojis in code or output.
package commands
