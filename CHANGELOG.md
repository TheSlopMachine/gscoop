# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- GitHub scaffolding: `ci`, `release`, and `drift` workflows, issue and pull
  request templates, `CODEOWNERS`, and `CONTRIBUTING.md` / `SECURITY.md`.
- Minimal `.golangci.yml` (govet, gofmt, misspell, ineffassign) for optional
  local lint runs; CI gates on `go build`, `go vet`, `go test`, and `gofmt`.
- Weekly manifest-corpus drift check (build plus `internal/manifest` tests,
  no network) as a placeholder for the full differential harness against
  `ScoopInstaller/Scoop@master`.
- Tagged-release pipeline: Windows builds of `./cmd/gscoop`, SHA-256
  checksums, and GitHub release upload on `v*` tags.

### Fixed

- Bucket sync tolerates unstaged edits: clean buckets pull through the
  engine, dirty buckets pull through `git.exe pull -q`, dirty buckets
  without `git.exe` skip without failing siblings. Core update treats
  tracked modifications only as uncommitted changes; untracked files no
  longer block.
- `scoop install -i` skips dependency resolution and installs only the
  listed apps; `scoop reset` relinks the `current` junction and recreates
  shims per app instead of reporting success without changes.
- `scoop cache rm` removes matching cache entries; `scoop download`
  honors `--force` and `--skip-hash-check`; `scoop import` replays
  configs, buckets, apps, and holds from the scoopfile.
- `scoop help <command>` routes through help output; `scoop --version`
  reports the newest `CHANGELOG.md` version; `DEBUG` gating matches
  classic case-insensitive checks; size output uses thousands grouping.
- `scoop update` runs the full install pipeline: pre/post-install hooks,
  installer scripts, environment updates, persist, and notes replay from
  the manifest; shim names match classic leaf plus extension-strip rules;
  download, hash, and extraction stages report progress. Shim failures
  remain fatal.
- Command parity: `info` preserves manifest suggest order; `export`
  counts bucket manifests like classic; `install`/`uninstall`/`hold`
  enforce global admin rights; `reset` warns and exits 0; `shim add`
  honors `--`/`--%` terminators and `shim list -g` shows globals only;
  `home` prints nothing on success; `create` with no URL shows help and
  exits 0; command lookup is case-insensitive; UNC scoopfiles import
  from disk.
- Literal commands (`install`, `info`, `uninstall`, `hold`, `download`,
  `home`, `prefix`, `cat`, `depends`) treat `*?[]` as not-found instead
  of creating `apps\*` or returning unrelated manifests.

## [0.1.0] - pre-release, no tag published yet

Scaffold release. The pure-Go Scoop reimplementation builds and its test
suite passes. `bucket/gscoop.json` still carries zero placeholder hashes
until the first `v*` tag publishes real release checksums. Signed binaries
and the nightly drift harness are roadmap items, not shipped artifacts.

[Unreleased]: https://github.com/TheSlopMachine/gscoop/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/TheSlopMachine/gscoop/releases/tag/v0.1.0
