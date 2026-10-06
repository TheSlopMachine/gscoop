<!-- Pull request template for gscoop. -->

## Conventional-commit title

<!-- e.g. feat: ..., fix: ..., docs: ..., test: ..., chore: ... -->

## Summary

## Test evidence

<!-- Paste the output of `go build ./...`, `go vet ./...`,
     `go test ./... -count=1`, and the `gofmt -l` gate. -->

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test ./... -count=1` passes
- [ ] `gofmt -l` reports no files

## Checklist

- [ ] Title uses a conventional-commit prefix
- [ ] `CHANGELOG.md` updated under `[Unreleased]` if user-visible
- [ ] No emojis, LF endings, CGO-free build
