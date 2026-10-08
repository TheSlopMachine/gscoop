# Smoke seed: tiny 1.0.0

Isolated root built inline by the smoke workflow. No checkout state is reused.

## Layout

- `buckets/main/bucket/tiny.json`: copy of `testdata/smoke/tiny.json`.
- `apps/tiny/1.0.0/scoop-manifest.json`: installed manifest copy.
- `apps/tiny/1.0.0/scoop-install.json`: install record (`architecture`, `url`, `bucket`).
- `apps/tiny/current/scoop-manifest.json`: junction target manifest read.
- `cache/tiny#1.0.0#<id>`: cached download entry.
- `shims/`: entry for `tiny.cmd`.
- `config.json`: `{ "debug": false }`.

## Environment

Root resolution consumes `SCOOP`, `SCOOP_GLOBAL`, `SCOOP_CACHE`, `XDG_CONFIG_HOME`, `SCOOP_ARCH`.
`SCOOP_ARCH` selects the manifest architecture. `XDG_CONFIG_HOME` locates `config.json`.

## Assertions

- `persist/tiny/data` exists after install.
- `apps/tiny/1.0.0/.smoke-pre` exists after `pre_install`.
- `apps/tiny/1.0.0/.smoke-post` exists after `post_install`.
- `SMOKE_TINY=1` is set after `env_set` application.
- `uninstall -p` removes `persist/tiny`.
