# stockctl CLI improvement plan

**Objective:** make `stockctl` dependable for unattended Yahoo daily-history
seeding and consistently usable by people, scripts, and the existing Hermes
brief launcher.  `stockctl` remains the data-plane owner: cache integrity,
single-writer coordination, checkpoint/resume, rate limiting, retries, and
observable terminal state live here. Hermes only starts one process, bounds its
wall-clock lifetime, and reports the resulting delivery status.

## Current state (evidence)

The repository is a clean nested Git repository on `main` at `2a3e6a2`
(2026-07-23); `git status --short` was empty during this review. `go test ./...`
passes locally. CI runs `go test -race ./...`, builds, and syntax-checks the
thin seed launcher ([`.github/workflows/test.yml`](../../.github/workflows/test.yml)).

The recent reliability work is substantial and should be preserved, not rebuilt:

- `seed history` persists an atomically written v3 JSON checkpoint after each
  result, records coverage intent, supports deadline/cancellation, permits only
  sequential work, and rejects `--no-cache` ([`cmd/seed.go`](../../cmd/seed.go),
  [`cmd/seed_test.go`](../../cmd/seed_test.go)). It correctly treats stale-cache
  fallback as incomplete seed work.
- The provider stack is Yahoo (per-process token bucket plus bounded full-jitter
  retries) → circuit breaker → disk cache ([`internal/marketdata/builder.go`](../../internal/marketdata/builder.go),
  [`yahoo.go`](../../internal/marketdata/yahoo.go),
  [`circuitbreaker.go`](../../internal/marketdata/circuitbreaker.go)). Cache
  writes are temp-file + `Sync` + rename and per-entry advisory locks prevent
  cache corruption ([`diskcache.go`](../../internal/marketdata/diskcache.go)).
- `scan` fetches a ticker once into an immutable snapshot shared by strategies,
  and reports scan provenance/quality in the standard JSON envelope
  ([`cmd/scan.go`](../../cmd/scan.go), [`internal/output/envelope.go`](../../internal/output/envelope.go)).
- The brief collector correctly uses an exchange calendar, an explicit
  prior-session `--date`, a process lock, and a data-quality gate
  ([`automation/market_morning_brief.py`](../../automation/market_morning_brief.py)).
  The seed wrapper is intentionally an `exec`-only launcher
  ([`automation/stockctl_weekend_seed.py`](../../automation/stockctl_weekend_seed.py)).

Important inconsistencies remain: checkpoint access has no run-level lock, so
two seed processes can both read/update the same state file; rate limiting is
per process (the agent skill explicitly warns about this); seed writes a bare
summary rather than the normal `{meta,results,errors,warnings}` envelope;
`cache stats` only reports file count/size and `cache clear` silently mutates
without JSON confirmation; and `pairs`/`backtest --strategy` use independent
fetch paths, with the latter re-fetching matching symbols
([`cmd/pairs.go`](../../cmd/pairs.go), [`cmd/backtest.go`](../../cmd/backtest.go)).

## Prioritized findings

### P0 — unattended seed correctness and truthful terminal state

1. **Serialize seed ownership before reading the checkpoint.** Per-cache-file
   locks protect data files, but not `seed-history-state.json`; concurrent seed
   launches can duplicate requests and lose checkpoint updates. This directly
   conflicts with a safe scheduled seed.
2. **Make seed outcome machine-readable and inspectable.** A bare stdout
   `seedSummary` has no schema/version/command metadata, no per-failure detail,
   and no supported `status` command. The runbook asks operators to edit JSON
   manually, which is too risky for recovery.
3. **Verify cache entries before declaring a ticker successful.** A successful
   provider call currently proves only that it returned data. The seed should
   validate non-empty, ordered daily bars and requested coverage before it marks
   completion, then expose validation failures as terminal/pending state.

### P1 — predictable CLI contracts and heavy-workflow consistency

1. **Standardize every mutating/integratable command on validated output and
   exit semantics.** `scan` has the mature envelope/quality contract, while
   `seed`, `cache clear`, and several table/CSV fall-through paths do not.
   Invalid `--output` is not centrally rejected.
2. **Reuse the existing snapshot path where it pays for itself.**
   `backtest --strategy` calls legacy `runScreenerV2` once per strategy and
   re-fetches passers; it can reuse `fetchScanSnapshot` and its quality/error
   accounting. `pairs` should preserve partial-fetch errors and dates rather
   than only logging skipped symbols.
3. **Turn cache statistics into health diagnostics.** File count and byte size
   cannot answer whether the scheduled seed covered the current universe,
   whether cache files decode, or which items are stale/corrupt.

### P2 — confidence, discoverability, and safe operations

1. Add command-level contract tests (Cobra execution with temp config/state),
   especially JSON stdout/stderr separation and non-zero incomplete outcomes.
   Current tests are strong for provider/seed primitives but sparse for root,
   cache, quote, inspect, pairs, and output contracts.
2. Align README, agent skill, help text, and the runbook around a small set of
   copy/paste-safe commands. In particular, document `seed status`/recovery
   instead of manual JSON mutation, and state that seed currently supports
   `india` and `us` only.
3. Add a release-quality version value and reproducible build target only after
   command contracts stabilize. The current default is `dev` in
   [`cmd/version.go`](../../cmd/version.go); no release packaging is present.

## Target architecture and CLI UX principles

Keep the current narrow provider interfaces. Do **not** introduce a general job
framework, database, queue service, or Hermes data-plane retry loop.

```text
Hermes / cron (one process, timeout, delivery alert)
                         |
                         v
stockctl seed history --state-file ...
  run lease -> checkpoint state machine -> provider stack -> validated cache
                  |                         |
                  +-> JSON envelope/status  +-> Yahoo limiter/retry/breaker
```

- **One owner, durable progress:** a state-file-adjacent non-blocking lease
  prevents duplicate seed owners. On SIGINT/deadline, persist pending work,
  release the lease, emit a complete summary, and return non-zero.
- **Truth over availability:** stale cache may serve interactive scans with
  provenance; it never counts as a fresh seed success. Validate before success
  and distinguish terminal failure, deferred retry, interruption, and complete.
- **Stable automation contract:** every `--output json` command emits exactly
  one envelope on stdout; diagnostics/progress remain stderr and obey
  `--quiet`. Unknown output formats are usage errors.
- **Safe repair by command:** status/verify are read-only. Any reset/clear is
  explicit, scoped, dry-runnable, and returns the affected count in JSON.
- **Reuse proven code:** scan stays the shared snapshot implementation;
  backtest strategy mode consumes it rather than cloning it. Provider policy
  stays in `internal/marketdata`, not Python/Hermes.

## Phased implementation roadmap

Each checkbox is intentionally a 2–5 minute, independently testable change.
Run the listed focused test after each task; make one commit at each stated
boundary (combine only the tasks within that boundary). Do not change existing
JSON field meanings without the compatibility steps below.

### Phase 1 — exclusive, observable seed runs (P0)

**Commit 1: `feat(seed): serialize state-file ownership`**

- [ ] Add `seedRunLease` in `cmd/seed.go`, using `flock(LOCK_EX|LOCK_NB)` on
  `<state-file>.lock`; acquire it before `loadSeedCheckpoint`, release with
  `defer`, and return a typed/sentinel `ErrSeedAlreadyRunning` with the lock
  path. Preserve the existing cache-entry locks.
  - Test: `cmd/seed_test.go` opens one lease, executes a second command against
    the same temp state file, asserts no provider calls and `errors.Is`.
  - Validate: `go test ./cmd -run 'Seed.*(Lease|Running)'`.
- [ ] Add `run_id`, `attempted`, and a bounded `failures` array (`ticker`,
  `status`, `attempts`, `last_error`, `next_retry`) to seed result types in
  `cmd/seed.go`; collect deterministically in ticker order without logging each
  ticker to stdout.
  - Test: a mixed fake provider yields stable failure ordering and correct
    attempted/succeeded/failed/pending totals.
  - Validate: `go test ./cmd -run TestSeedHistorySummary`.
- [ ] Replace seed’s bare JSON encoding with `output.Envelope` in
  `cmd/seed.go` (`meta.command="seed-history"`, market list and duration in
  results metadata; summary/failures in `results`); return the existing
  non-zero incomplete error after writing the envelope. Update the focused
  seed tests to decode the envelope.
  - Test: stdout decodes once as an envelope for success and deadline paths;
    stderr is empty with `--quiet`.
  - Validate: `go test ./cmd -run 'TestSeedHistory.*(JSON|Deadline)'`.

**Commit 2: `feat(seed): validate seeded history before checkpoint success`**

- [ ] Add unexported `validateSeedHistory(data, period)` in `cmd/seed.go`:
  reject empty data, zero/invalid dates, non-ascending/duplicate days, and
  insufficient period start coverage for `5y`/`10y`; for `max`, require a
  non-empty valid sequence but do not invent a pre-listing date requirement.
  Call it after `GetHistoryWithProvenance` and before marking success.
  - Test: table tests cover invalid ordering/duplicates, valid sparse trading
    dates, five-year undercoverage, and valid `max` history.
  - Validate: `go test ./cmd -run 'TestValidateSeedHistory|TestSeedHistory.*Coverage'`.
- [ ] Classify validation failures as terminal data failures in the checkpoint
  and final envelope, while keeping stale fallback and context deadline pending
  as today.
  - Test: fake malformed history is `failed`; stale fallback stays retryable
    until max attempts.
  - Validate: `go test ./cmd -run TestSeedHistory`.

### Phase 2 — safe status and cache integrity (P0/P1)

**Commit 3: `feat(seed): add read-only seed status and verify`**

- [ ] Add `stockctl seed status --state-file PATH --output json|table` in
  `cmd/seed.go`. It loads without creating directories, returns checkpoint
  identity, `updated_at`, per-status counts, due-retry count, oldest pending,
  and sample failures (maximum 20); missing state is a successful `not_found`
  result, not an error.
  - Test: construct a temp checkpoint and assert aggregate/count/sample fields;
    assert no file is created for missing input.
  - Validate: `go test ./cmd -run TestSeedStatus`.
- [ ] Add `stockctl seed verify --market ... --state-file PATH --output json`
  that reads known cache files through a new read-only decoder/helper in
  `internal/marketdata/diskcache.go`, checks decode/date/order/coverage, and
  reports `valid`, `missing`, and `corrupt` counts without Yahoo calls or cache
  writes. Limit this first version to the supported seed markets.
  - Test: `internal/marketdata/diskcache_test.go` exercises valid/corrupt gob
    entries; `cmd/seed_test.go` proves no provider factory invocation.
  - Validate: `go test ./cmd ./internal/marketdata -run '(SeedVerify|Cache.*Validate)'`.
- [ ] Update `docs/operations/yahoo-data-runbook.md` to replace manual state
  editing with `seed status` and `seed verify`. Retain a documented, deliberate
  operator escalation path rather than adding automatic destructive reset.
  - Validate: `rg -n 'seed status|seed verify|edit.*state' docs/operations README.md`.

**Commit 4: `feat(cache): report health and make clear auditable`**

- [ ] Extend `internal/marketdata.CacheStats` and `GetCacheStats` to count
  decodable, corrupt, and lock files separately; do not decode every file by
  default unless `cache stats --verify` is passed. Add a `--verify` flag in
  `cmd/cache.go` and return these fields in the existing JSON envelope.
  - Test: temp cache with one valid gob and one corrupt gob has exact counts.
  - Validate: `go test ./internal/marketdata ./cmd -run 'Cache.*(Stats|Verify)'`.
- [ ] Make `cache clear` require `--yes`; add `--dry-run` and JSON envelope
  containing `matched`, `removed`, `market`, and `dry_run`. Preserve the
  existing market filter behavior and never remove `.lock` files in this task.
  - Test: no `--yes` changes nothing; dry-run changes nothing; scoped remove
    counts only matching gob files.
  - Validate: `go test ./cmd ./internal/marketdata -run CacheClear`.

### Phase 3 — consistent command contracts and data paths (P1)

**Commit 5: `fix(cli): validate output and normalize command results`**

- [ ] Add `output.ParseFormat(string) (Format, error)` in
  `internal/output`, accepting only `table`, `json`, and `csv`; validate the
  persistent flag in `cmd/root.go` before command work. Keep `markets` and
  `version` config-free.
  - Test: table-driven parser tests and root-command execution assert invalid
    output returns a JSON error envelope when `--output json` was requested,
    otherwise stderr/error exit.
  - Validate: `go test ./cmd ./internal/output -run '(Output|Root)'`.
- [ ] Audit command branches in `cmd/cache.go`, `cmd/quote.go`, `cmd/tickers.go`,
  `cmd/pairs.go`, and `cmd/backtest.go`: JSON always uses the standard envelope;
  CSV is either implemented with an explicit output path in `runDir` or rejected
  as unsupported for that command—never silently rendered as a table.
  - Test: command-contract table covers JSON schema/command name and CSV
    behavior for each command using fakes/temp inputs.
  - Validate: `go test ./cmd -run TestCommandOutputContracts`.

**Commit 6: `refactor(backtest): use scan snapshot for strategy mode`**

- [ ] Extract only the already-existing scan preparation needed by strategy
  backtesting from `cmd/scan.go` (universe suffixing, `fetchScanSnapshot`,
  `runScreenerFromSnapshot`) into package-private helpers; remove
  `runScreenerV2` after call sites are migrated. Do not change screener APIs.
  - Test: counting provider proves `backtest --strategy all` fetches each ticker
    plus benchmark once, rather than once per screener.
  - Validate: `go test ./cmd -run '(Snapshot|Backtest.*Strategy)'`.
- [ ] Return a backtest strategy-mode JSON result containing the scan
  `data_quality`, fetch errors, `entries_considered`, and `entries_used`; fail
  before optimization if benchmark/data quality does not meet an explicit,
  documented threshold. CSV mode remains unchanged except for normal envelope
  metadata.
  - Test: missing benchmark and excessive ticker failures have deterministic
    non-zero JSON outcomes; healthy fixture retains current optimization shape.
  - Validate: `go test ./cmd -run TestBacktest`.
- [ ] Make `pairs` collect skipped-symbol errors and data-as-of/provenance into
  its JSON envelope; retain partial-result success only when at least two valid
  series remain, and make no claim that independently truncated series are
  date-aligned.
  - Test: fake provider partial failure produces result + `errors`; mismatched
    dates are joined by date (not merely tail-aligned) before correlation.
  - Validate: `go test ./cmd ./internal/pairs -run '(Pairs|Correlation)'`.

### Phase 4 — operational hardening and discoverability (P2)

**Commit 7: `test(cli): protect automation-facing contracts`**

- [ ] Add fixture-based Cobra integration tests under `cmd/` for root timeout,
  quiet JSON stdout/stderr separation, `seed status`, cache clear confirmation,
  and scan quality-envelope compatibility with
  `automation/market_morning_brief.py`.
  - Validate: `go test -race ./...` and
    `python3 -m unittest automation/test_market_morning_brief.py`.
- [ ] Extend CI in `.github/workflows/test.yml` to run the Python unit test and
  `go vet ./...`; keep the existing race test and build.
  - Validate locally: `go vet ./...; go test -race ./...; python3 -m unittest automation/test_market_morning_brief.py`.

**Commit 8: `docs(cli): publish the supported unattended workflow`**

- [ ] Update `README.md`, `.agent/skills/stock-analysis/SKILL.md`, Cobra long
  help in `cmd/seed.go`/`cmd/cache.go`, and the runbook with one canonical seed
  command and its `status`/`verify` follow-ups. State clearly: one seed process,
  `--workers 1`, stockctl owns retries/cache/state, Hermes owns only launch,
  timeout, and terminal delivery.
  - Validate: `go run . seed history --help` and `go run . seed status --help`
    show the documented flags; `rg -n 'Hermes.*(retry|cache|checkpoint)' README.md docs .agent` shows no conflicting ownership.
- [ ] Add a `Makefile` only if maintainers want a stable local entry point:
  `test`, `test-race`, `build`, and `test-automation`; pass version through
  `-ldflags` in `build` but preserve `go build .` support. Otherwise record the
  chosen direct commands in README and do not add packaging machinery.
  - Validate: selected build path produces `stockctl version --output json`
    with a non-`dev` release value in CI release builds.

## Risks, migration, and backward compatibility

- **Checkpoint migration:** retain v1/v2→v3 coverage reset behavior in
  `loadSeedCheckpoint`. Add new optional result fields only; if a future state
  version is needed, provide an explicit, tested migration and never silently
  reinterpret completed `max` entries.
- **Existing automation:** the current seed wrapper forwards arguments unchanged,
  so it needs no data-plane logic. Update any consumer that parses bare seed
  stdout to read `results.summary`; rollout the envelope behind no flag only
  after updating the runbook/wrapper consumers in the same commit.
- **Lock behavior:** fail fast with an actionable `seed already running` error;
  do not block indefinitely and do not let a stale lock file block (advisory
  lock release on process exit handles this). This is compatible with Hermes
  supervision and safe manual retries.
- **Coverage validation:** listed/de-listed instruments and Yahoo listing dates
  make a literal `max` start unknowable. Validation must only prove structural
  integrity for `max`; it must not falsely fail a legitimate short history.
- **Cache inspection cost:** default `cache stats` remains cheap. Full gob
  decoding is opt-in (`--verify`) and must tolerate unreadable files without
  mutating them.
- **Output consumers:** preserve schema version `2.0` and existing scan fields.
  New fields are additive; only increment schema/version after a documented
  breaking change. Keep human table output stable where practical.
- **Network tests:** use injected providers and temp cache/state files; no CI
  test may call Yahoo or depend on market hours.

## Acceptance criteria

1. Starting a second `seed history` for the same state file exits promptly,
   makes zero Yahoo/provider calls, and reports a machine-readable conflict.
2. A seed interrupted by deadline/cancellation emits one JSON envelope, leaves
   all unfinished work resumable, and later resumes without re-fetching verified
   successful tickers for the same coverage intent.
3. No ticker becomes seed-successful unless its cached daily history is
   structurally valid and covers the requested bounded period; stale fallback
   never satisfies seed completion.
4. `seed status`, `seed verify`, and `cache stats --verify` provide enough
   read-only evidence to diagnose a run without editing gob/JSON files.
5. `cache clear` cannot mutate without `--yes`, and its JSON result names what
   it matched/removed.
6. Every command either emits the standard JSON envelope or explicitly rejects
   an unsupported format; `--quiet --output json` leaves stdout parseable.
7. Strategy backtests share a one-fetch-per-ticker snapshot and preserve quality
   failures instead of treating them as ordinary no-signals.
8. `go vet ./...`, `go test -race ./...`, Python automation tests, build, and
   seed-launcher compilation pass with no live Yahoo calls.

## Recommended first milestone

Deliver **Phase 1 / Commit 1** first: the run-level state-file lease plus
machine-readable seed envelope. It closes the only direct race between two
scheduled/manual seed owners, gives Hermes an unambiguous terminal artifact,
and is small enough to ship with deterministic unit tests before touching cache
format or general command UX.
