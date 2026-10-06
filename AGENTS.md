# AGENTS.md

Guidelines for AI agents in `gscoop/`. Read before any change. Treat every rule as absolute. No exceptions. No reinterpretation.

## 1. Project

`gscoop` — native Go replacement for the Scoop command-line installer; single static binary.

- Module `github.com/TheSlopMachine/gscoop`, Go 1.22 floor, `CGO_ENABLED=0`; Windows is the primary platform.
- Entry point `cmd/gscoop`; packages under `internal/` mirror classic `lib/` and `libexec/` areas.
- Reads and writes the classic Scoop on-disk state bit-for-bit; both implementations alternate on one machine.
- `powershell.exe` is the only external process, used solely to execute manifest lifecycle hooks.
- `bucket/gscoop.json` hashes stay zero-placeholders until the first `v*` tag produces real checksums.
- Binding docs: `Scoop-Go-Rewrite-Technical-Plan.md` (behavior contract), `spec/` (frozen state formats), `CHANGELOG.md` (release source).

## 2. Structure

```
cmd/gscoop            entry point, dispatch mirroring bin/scoop.ps1
internal/cli          argument parser and help text
internal/commands     one file per subcommand
internal/config       config.json keys, case-insensitive lookup
internal/state        paths, installed apps, version resolution
internal/manifest     parse, arch resolution, schema validation
internal/bucket       local and known buckets, manifest enumeration
internal/gitengine    go-git engine plus git.exe fallback
internal/download     concurrent fetcher, cache naming, hashes
internal/extract      archive registry, msiexec bridge for MSI
internal/install      install and uninstall pipelines, persist, env
internal/hook         PowerShell hook runner, sole os/exec user
internal/shim         embedded shim binaries, wrapper writers
internal/search       in-binary search plus SQLite cache
internal/version      Compare-Version port plus property tests
internal/deps         dependency resolution order
internal/update       app updates, bucket sync, self-update, cleanup
internal/doctor       extended checkup for distribution
internal/lnk          Start-Menu shortcut writer
internal/junction     NTFS junction helpers
internal/ui           severity prefixes, TTY color handling
spec/                 binding docs: frozen contract formats
testdata/             fixtures and golden outputs
spikes/               feasibility probes, not shipped code
bucket/               manifest for gscoop itself
.github/workflows/   CI, release, and drift definitions
CHANGELOG.md          binding doc: release notes source
```

Keep changes shallow. Do not touch internals unless the task requires it.

## 3. Behavioral Rules

### 3.1 Wording

Apply this section to all agent output: commit messages, PR descriptions, code comments, log strings, UI copy, CLI help/output, error messages, and doc edits.

#### 3.1.1 No filler phrases

Never pad a sentence without adding information.

DON'T:
- "In order to fix this issue, we need to update the parser."
- "It's worth noting that the cache is LRU-based."
- "Please note that this change also updates the parser."
- "As you can see, the test now passes."

DO:
- "Fix the issue by updating the parser."
- "The cache is LRU-based."
- "Also update the parser."
- "The test now passes."

#### 3.1.2 No implementation-detail asides in UI or CLI

Surface actions and facts only in UI text, CLI output, log lines, and error messages. Never narrate internal conditions there.

DON'T:
- `"Skip validation... (because SKIP_VALIDATION was true)"`
- `"Load models... (isCacheValid == false)"`
- `"Retry request... (attempt < maxRetries)"`
- `"Save record... (record.token != "")"`

DO:
- `"Skip validation"`
- `"Load models"`
- `"Retry request"`
- `"Save record"`

Move the reasoning to a code comment, a commit message, or drop it. Log debuggable conditions at debug level with structured fields (`reason=rate_limited`).

#### 3.1.3 No casual or vague wording

Use precise technical terms in code, comments, commits, logs, and docs. Banned examples: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

DON'T:
- "This blows up if the token is empty."
- "Wire the new adapter into the router."
- "Sweep stale entries on startup."
- "Spin up a worker to poll the queue."

DO:
- "This fails if the token is empty."
- "Register the new adapter with the router."
- "Remove stale entries on startup."
- "Start a worker to poll the queue."

### 3.2 Session Start

Open every session by identifying the platform and shell. Never assume them.

- Query the runtime environment first (OS, shell, working directory); training defaults do not apply.
- The shell a tool runs can differ from the platform default. Detect it; never infer it from the tool name.
- Write every command in the syntax of the detected shell. On PowerShell: chain commands with `;`, quote paths with `"..."`, never use Unix-isms (`head`, `cat`, `VAR=x cmd`). On POSIX shells: never use PowerShell-isms.
- Before state-contract changes, read `Scoop-Go-Rewrite-Technical-Plan.md` and the matching `spec/` file.
- Add a `CHANGELOG.md` entry under `[Unreleased]` for user-visible changes.

### 3.3 Commands and Prohibitions

#### 3.3.1 Targets

The agent runs commands marked `agent` directly. Commands marked `human` follow §3.3.3.

| Command | Access | Purpose |
|---|---|---|
| `go build ./...` | agent | full build |
| `go vet ./...` | agent | static analysis |
| `go test ./... -count=1` | agent | full test suite |
| `go test ./<pkg>/ -v` | agent | single package via package path |
| `gofmt -l .` | agent | format gate, empty output required |
| `golangci-lint run ./...` | agent | optional lint, not a CI gate |
| `go run ./cmd/gscoop <subcommand>` | human | run entrypoint, mutates Scoop state |
| `v*` tag via release.yml | human | release publish and checksums |
| hash updates in `bucket/gscoop.json` | human | manifest rewrites after release |

#### 3.3.2 Raw commands

This project has no custom tools. Use the raw commands in §3.3.1.

#### 3.3.3 Human-only handoff

The agent performs only the tasks listed under **Agent may**. A task listed under **Human only**, or absent from both lists, is reserved for the human: STOP and ask. Never perform it. Never approximate it with raw commands, scripts, or wrappers.

**Agent may:**
- Edit source (`cmd/`, `internal/`), tests (including `testdata/` goldens), and docs (`README.md`, `CHANGELOG.md` entries, `spec/` with plan reference).
- Run the commands marked `agent` in §3.3.1.
- Delegate implementation phases to subagents per §3.6.

**Human only:**
- Run the `gscoop` binary and any subcommand (install, update, reset, cleanup, cache).
- Release publish: `v*` tag via `release.yml` and `CHANGELOG.md` release section rename.
- Manifest rewrites: hash updates in `bucket/gscoop.json` after a release.
- Push commits to the remote.

| DO | DON'T |
|---|---|
| "Please run `go run ./cmd/gscoop <subcommand>` and paste the output." | Executing install, update, reset, or cleanup against real Scoop state, any script invoking them |

Human-run protocol:
1. State exactly what is needed: command, endpoint, log line, or behavior.
2. Ask the human to run it and report back (terminal output, logs, result).
3. Interpret the report before proposing the next step.

#### 3.3.4 Runtime and verification

The agent runs checks and tests through the commands marked `agent` in §3.3.1. The agent never starts the project (server, binary, GUI, worker). For runtime facts, follow the human-run protocol in §3.3.3.

Verification gate, in this order: `go build ./...` -> `go vet ./...` -> `gofmt -l .` -> `go test ./... -count=1`. Run the gate after every change.

#### 3.3.5 Prohibitions

| # | Prohibition | Detail |
|---|---|---|
| 1 | Kill processes | No `kill`, `pkill`, `taskkill`, `Stop-Process`. Use the project's stop command (§3.3.1) so tracking files stay consistent; if none exists, ask the human. Warn before any command that stops running processes when the user may have work in flight. |
| 2 | Delete state files | Never remove databases, data directories, or persisted state files, including local development ones (`~\scoop`, `%ProgramData%\scoop`, `%SCOOP_CACHE%`, `scoop.db`, `scoop.lock`). If the state is corrupt, ask the human to delete it. |
| 3 | Run destructive git commands | No `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend. Commit only if explicitly instructed, for that exact commit only. |
| 4 | Litter the project | No `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree. Use the system temp directory or an external scratch directory. Delete temporary verification files when done. |
| 5 | Redirect output to nul or /dev/null | Breaks on Windows and hides diagnostics on every OS. |
| 6 | Truncate diagnostics output with tail/head | Diagnostics matter and truncating wastes time. NEVER truncate them. |
| 7 | Fall back silently | NEVER swallow a failure and continue on a fallback path. Surface every failure as an error — return it to the caller, log it, or both — or route it to an explicit, named on-fail branch. Never fall through unannounced. |
| 8 | Match errors by string | NEVER match error codes or kinds by message substrings. Define typed or sentinel errors and match by type or code. |
| 9 | Patch a weak contract in the consumer | Repetitive null guards or fallbacks over a producer's data shapes mean the contract is wrong. Fix the producer to return consistent shapes (collections never null, objects never null when the schema promises them). Consumer guards stay only for genuinely optional local state. |
| 10 | Create stray files from the shell | NEVER let a shell command create a file: no `>` / `>>` / `Out-File` redirection, no heredocs, no `2>/dev/null`, no unquoted fragments that resolve to filenames (`nul`, `null`, command text as filename). Read command output from the tool result, never from disk. |
| 11 | Pass env vars with shell syntax | NEVER `VAR=value <tool>` (Unix-only) and NEVER `$env:VAR="value"; <tool>` (PowerShell-only, leaks state into the session). Pass variables through the tool's own argument syntax (`<tool> <target> VAR=value`, `--flag`), which works in every shell and stays scoped to one invocation. If the tool offers none, ask the human. |
| 12 | Act on questions | Answer questions with words. A question never authorizes edits, runs, installs, or any other side effect — not even an obvious follow-up. |
| 13 | Write files through executed scripts | Never create or patch files via throwaway scripts (inline-interpreter writers, heredoc generators, `sed` equivalents). Use the dedicated file tools so every change stays reviewable. |
| 14 | Perform human-only tasks | Never run, build, launch, or script a task listed under **Human only** in §3.3.3, or absent from both lists. Includes approximating it through raw commands, scripts, or wrappers. |

### 3.4 Edit Discipline

- Grasp the concept before the code. On ambiguity, ask instead of inferring from code literally.
- Re-read the region before every edit to the same file. Stale match text either fails to match or deletes neighboring code.
- Batch edits to one file only from a fresh read of every region; verify each result before the next batch.
- After deleting a function, grep the file's imports and includes immediately; orphaned imports are guaranteed, and the compiler or linter catches them a cycle later.
- Diff every edited file before moving on.
- Leave no scaffolding: delete draft notes, TODO placeholders, and reasoning asides left over from the edit itself.
- One simple shell command per call: no heredocs, no `VAR=x cmd`, no bare `echo`, no `2>/dev/null` (§3.3.5 #10, #11).
- A predicted runtime-only risk ships with its test in the same change (for example route patterns or config schemas that validate only at startup).
- Verify through execution whenever §3.3.4 permits: run the verification gate after implementing, fixing, or refactoring. When §3.3.4 forbids execution, state the commands the human must run.
- Attribute failures before fixing: when a check fails after the change, reproduce on the clean tree first with a scoped run. `git stash` is permitted for baselines but dangerous: confirm a clean `git status` before stashing, pop immediately after the baseline run, and prefer a separate worktree when unrelated work is in flight. A forgotten stash loses work silently.

### 3.5 Design Defaults

- One knowledge, one owner. Duplicated logic drifts toward bugs; unify instead of patching instances.
- Fail closed. Unknown state denies with a surfaced error, never a default success.
- The transaction boundary belongs to the domain operation that owns the invariant: one public operation, one transaction.
- Fix the contract where data is born. Consumer-side guards, display-side parsing, and anonymous DTOs duplicating models are producer bugs postponed (§3.3.5 #9).
- Persisted derived values are frozen. Functions whose outputs live in storage never change semantics without a migration; lock them with stability tests.
- Migrate stored shapes explicitly: version the container (`items_v2`) or declare a named one-shot migration function. Never hide backward-compat inside the function needing the new shape.
- Fix the class, not the instance. Systemic fixes run smaller than instance patches.
- Names must not lie. Rename when semantics change; a lying name is worse than none.

### 3.6 Subagent workflow

- Delegate every implementation phase to subagents; the coordinator plans, verifies, and integrates.
- Supply each subagent with complete, self-contained context; subagents share no dialogue history.
- Parallel agents receive exclusive file ownership (owned paths versus read-only paths); ownership violations fail the task.
- The coordinator never writes implementation code directly and never applies fixes that belong to a subagent rerun.
- Send troubleshooting and bug fixes to subagents by rerunning them with the failure attached.
