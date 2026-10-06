# Contributing to gscoop

## Prerequisites

- Go 1.22 or later (the version in `go.mod` is authoritative).
- Windows is the primary platform. `CGO_ENABLED=0` for every build.
- LF line endings on all files. Do not use emojis anywhere.

## Build and test

```powershell
$env:CGO_ENABLED = "0"
go build ./...
go vet ./...
go test ./... -count=1
```

The `gofmt -l` gate must report no files. The same four commands run in CI
(see `.github/workflows/ci.yml`).

Optional lint (not a CI gate):

```powershell
golangci-lint run ./...
```

## Commits

Use conventional commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
Reference the spec file or plan section the change implements when one
applies.

## Pull requests

- Fill in `.github/PULL_REQUEST_TEMPLATE.md`, including test evidence.
- Keep `bucket/gscoop.json` hashes untouched unless a real release produced
  them; the zero placeholders stand until the first `v*` tag.
- Update `CHANGELOG.md` under `[Unreleased]` for user-visible changes.
