# Yahoo Cache and Market-Data Reliability Implementation Plan

> **For Hermes:** Use `subagent-driven-development` and `test-driven-development` to implement this plan task-by-task.

**Goal:** Make large `stockctl` scans safe against Yahoo Finance throttling by maintaining a resumable weekly historical-data seed and serving on-demand scans from cached history plus a bounded delta only.

**Architecture:** Treat the local cache as the authoritative working store. **`stockctl` owns the weekly seed logic**: its `seed history` command owns batch selection, checkpoints, retry timing, rate limiting, resumability, and provider failure handling. Hermes owns only the weekend no-agent schedule, process timeout, run history, and delivery of a terminal failure alert. On-demand scans first read cached bars, request only the missing tail with a small overlap, merge/dedupe atomically, and expose provenance/staleness in JSON. A scan fetches each symbol once into a per-run snapshot, then executes all selected strategies against that snapshot.

**Tech stack:** Go, existing `oscarli916/yahoo-finance-api` provider, disk cache in `internal/marketdata/diskcache.go`, `golang.org/x/time/rate`, Cobra, Go tests, Hermes cron (no-agent scripts).

---

## Non-negotiable operating rules

1. **Never call Yahoo once per strategy.** One symbol/benchmark fetch maximum per run, regardless of `scan all`.
2. **On demand reads cache first.** It may fetch only the missing daily-bar delta after the last stored bar; request a 5-trading-day overlap for corrections/splits, then merge by timestamp.
3. **`stockctl` owns weekly seeding.** Its seed command is resumable, rate limited conservatively, and may run for several hours over a weekend; Hermes only schedules and supervises the process.
4. **A provider error must never masquerade as fresh data.** Cache age, last-bar date, stale fallback, upstream error, and coverage must appear in the JSON envelope.
5. **Retry budgets are bounded by an overall deadline.** Respect `Retry-After` if available; use exponential backoff + full jitter for 429, 5xx, and transient network errors.
6. **No concurrent writers.** Lock cache mutation across all CLI and seeder processes.
7. **Morning reports remain prior-close reports.** They use exchange-calendar-derived `data_as_of`; they do not claim live pricing.

## Backlog

| ID | Priority | Outcome | Depends on |
|---|---|---|---|
| MD-01 | P0 | Cache/provenance contract and atomic process lock | — |
| MD-02 | P0 | Delta-only fetch + merge/dedupe | MD-01 |
| MD-03 | P0 | One fetch snapshot shared by all scan strategies | MD-02 |
| MD-04 | P0 | Stockctl weekend seed command with checkpoint/resume | MD-01, MD-02 |
| MD-05 | P1 | Retry/backoff, context cancellation, correct circuit-breaker probing | MD-01 |
| MD-06 | P1 | Truthful JSON data-health and quality gates | MD-01–03 |
| MD-07 | P1 | Market policy: price floors and exchange data-as-of semantics | MD-06 |
| MD-08 | P2 | CI, operational docs, and safe weekend cron | MD-04–06 |
| MD-09 | P2 | Restore real sector mappings | independent |

---

## MD-01 — Cache/provenance contract and process lock

**Objective:** Make cache reads/writes observable and safe across CLI processes.

**Files:**
- Modify: `internal/marketdata/diskcache.go`
- Modify: `internal/marketdata/types.go`
- Create: `internal/marketdata/cache_lock.go`
- Create: `internal/marketdata/cache_lock_test.go`
- Create: `internal/marketdata/diskcache_test.go`

**Implementation:**
- Add a `HistoryResult` / `CacheProvenance` returned with bars: `Source` (`seed`, `cache`, `delta`, `stale_fallback`), `FetchedAt`, `LastBarDate`, `CacheAge`, `UpstreamError`, and `Stale`.
- Use an advisory lock rooted beside the cache directory. Readers may be concurrent; acquire an exclusive lock for merges/writes. Return a typed timeout error rather than racing writes.
- Replace direct provider-layer stderr writes with provenance returned to the command layer; preserve `--quiet` behavior.

**Tests (TDD):**
1. A fresh cache hit reports `Source=cache`, correct last-bar date, and `Stale=false`.
2. A failed delta fetch with old cached bars reports `Source=stale_fallback`, `Stale=true`, and a non-empty upstream error.
3. A second writer cannot enter while the first lock is held.

**Acceptance:** `go test ./internal/marketdata -run 'Cache|Lock'` passes; no stale fallback is invisible in structured output.

---

## MD-02 — Delta-only history refresh

**Objective:** Do not re-download five years of history when cached daily bars already exist.

**Files:**
- Modify: `internal/marketdata/diskcache.go`
- Modify: `internal/marketdata/yahoo.go`
- Create: `internal/marketdata/history_merge_test.go`

**Implementation:**
- Add `MissingRange(lastCachedBar, requestedEnd)` which requests from five trading days before the last cached bar through the requested end date.
- Merge bars by normalized daily timestamp; prefer newly fetched bars inside the overlap. Sort ascending and write atomically via temp file + rename.
- If cache is current for the requested `--date`, make **zero** Yahoo requests.
- Preserve the existing initial seed behavior only when no cache exists.

**Tests (TDD):**
1. A 100-bar cache with requested end equal to last bar causes no provider call.
2. A cache ending Friday and requested end Tuesday requests only the overlap/tail, not the original five-year start.
3. Overlapping refreshed bars replace old values and do not create duplicates.
4. Partial delta failure leaves the prior cache intact and returns stale provenance.

**Acceptance:** instrumented provider test proves an on-demand repeat scan performs zero history fetches when no new daily bar is available.

---

## MD-03 — Per-run market snapshot

**Objective:** `scan all` fetches each ticker and benchmark once, then runs all screeners over the identical snapshot.

**Files:**
- Modify: `cmd/scan.go`
- Modify: `internal/screener/screener.go`
- Create: `internal/screener/snapshot.go`
- Create: `cmd/scan_snapshot_test.go`

**Implementation:**
- Build `map[string]HistoryResult` for the selected universe before strategy evaluation; include one benchmark result.
- Change strategy execution to consume `Snapshot` data rather than invoke `FetchHistory` itself.
- Track fetch/data health once per ticker, independently of whether a strategy matches or passes `--min-score`.
- Keep strategy-specific score/pass metrics separate from universe coverage.

**Tests (TDD):**
1. Two selected strategies over three symbols result in exactly three symbol fetches plus one benchmark fetch.
2. `scan all` keeps `tickers_scanned` equal to universe size, not strategy count × universe size.
3. A successful non-match still contributes to complete/partial coverage correctly.

**Acceptance:** the current ~3,000-request warm-cache `scan all` pattern becomes at most one delta per symbol/benchmark, and normally zero on repeat pre-open runs.

---

## MD-04 — Stockctl weekend historical seed command

**Objective:** Put the complete seeding state machine in `stockctl`: queueing, checkpointing, retry/backoff, rate limiting, resumption, and structured terminal status. Hermes schedules exactly one long-running invocation on the weekend and does no data-plane orchestration.

**Files:**
- Create: `cmd/seed.go`
- Modify: `cmd/root.go`
- Modify: `internal/config/config.go`
- Create: `internal/marketdata/seed_state.go`
- Create: `cmd/seed_test.go`
- Create: `automation/stockctl_weekend_seed.py` (thin Hermes-facing launcher only)

**CLI contract:**
```bash
stockctl seed history --market india --market us \
  --start 2021-01-01 --workers 1 --rate 1.0 \
  --retry-budget 6 --deadline 6h --output json --quiet
```

**Implementation:**
- Persist checkpoint state per `(market, ticker)` in the stockctl cache/state directory: completed, pending, retry-at, last error, and latest cached bar. Use atomic writes.
- Enumerate the built-in universe and process each symbol directly through the cache-aware history layer; do not shell out to `scan` per batch.
- Retry transient failures with full-jitter exponential backoff and `Retry-After` support. Treat non-retryable symbol errors as terminal and include them in the final JSON.
- Default to **one worker and one request/second** for a cold seed. Make rate/worker overrides explicit and bounded.
- Checkpoint after every cache merge and after each retryable failure. If interrupted, the next `stockctl seed history` invocation resumes pending/retryable work without revisiting completed symbols.
- Return structured totals, coverage, retry counts, terminal failures, and cache freshness; exit non-zero only when the configured failure/deadline policy is breached.

**Hermes responsibility:** a thin no-agent wrapper launches this exact command on the weekend, applies the process timeout, preserves stdout/stderr and exit status, and delivers only the final failure alert. It does not own retries, batches, or seed state.

**Tests (TDD):**
1. A seed resumes from persisted state and skips completed symbols.
2. A 429 schedules a retry after `Retry-After` (or jittered delay fallback) without losing progress.
3. A cancellation persists a checkpoint before exit.
4. Re-running a completed seed only checks deltas, not historical ranges.
5. The Hermes wrapper invokes one stockctl command and returns its exit status unchanged.

**Acceptance:** an interrupted India+US seed can resume across weekends without an uncontrolled burst or duplicate historical downloads. All retry/resume behavior is testable in `stockctl` without Hermes.

---

## MD-05 — Provider retry, deadline, and circuit breaker

**Objective:** Correctly handle throttling/network faults without overlapping runs or probe bursts.

**Files:**
- Modify: `internal/marketdata/yahoo.go`
- Modify: `internal/marketdata/circuitbreaker.go`
- Modify: `internal/marketdata/builder.go`
- Create: `internal/marketdata/yahoo_retry_test.go`
- Create: `internal/marketdata/circuitbreaker_test.go`

**Implementation:**
- Use a context-aware HTTP provider/transport with a per-request timeout shorter than the run deadline.
- Classify retryable errors: 429, 5xx, connection reset, timeout. Respect `Retry-After` where exposed.
- Implement bounded exponential backoff with full jitter; stop when context deadline or retry budget expires.
- In `CircuitHalfOpen`, permit exactly one in-flight probe; fail/queue other callers until its result transitions the breaker.

**Tests (TDD):**
1. 429 + `Retry-After` waits the specified capped delay and retries once.
2. A canceled context prevents a queued retry and propagates cancellation.
3. Ten concurrent callers after breaker cooldown cause exactly one probe request.

**Acceptance:** no request retry continues after the command deadline, and half-open cannot make a multi-worker probe burst.

---

## MD-06 — Structured data quality and report gate

**Objective:** The agent and automation can reject stale/incomplete scan output safely.

**Files:**
- Modify: `internal/output/envelope.go`
- Modify: `cmd/scan.go`
- Modify: `automation/market_morning_brief.py`
- Create: `cmd/scan_data_quality_test.go`

**Implementation:**
- Add envelope fields: `data_as_of`, `provider_fetched_at`, `stale_tickers`, `cache_only_tickers`, `delta_refreshed_tickers`, `upstream_failures`, and age distribution.
- Make `tickers_scanned`, complete, partial, and failed mutually coherent and calculated once per ticker.
- Reject scheduled output when benchmark is stale, requested `data_as_of` is unavailable, or configured stale/failure thresholds are exceeded.

**Acceptance:** a Yahoo outage produces either a clearly labeled stale report (manual CLI only) or no scheduled Telegram delivery.

---

## MD-07 — Market policy and price floors

**Objective:** Apply market-aware trading rules rather than hard-coded USD assumptions.

**Files:**
- Modify: `cmd/scan.go`
- Modify: `internal/config/config.go`
- Modify: `internal/marketdata/markets.go`
- Modify: `internal/screener/*.go`
- Create: `cmd/scan_policy_test.go`

**Implementation:**
- Resolve price floor at startup: `--min-price` > config > market default.
- Pass effective policy into every screener; remove hard-coded `5.0` floors.
- Add IANA timezone/session metadata to market definitions for display and scheduling metadata only; continue using the external exchange calendar for actual holiday decisions.

**Acceptance:** `--min-price` changes results; India uses ₹10 by default; JSON includes currency and effective policy.

---

## MD-08 — CI, documentation, and weekend operation

**Objective:** Prevent regression and operate the seed unattended.

**Files:**
- Create: `.github/workflows/test.yml`
- Modify: `README.md`
- Modify: `automation/stockctl_weekend_seed.py`
- Create: `docs/operations/yahoo-data-runbook.md`

**Weekend job design:**
- Hermes no-agent cron, **Sunday 03:00 IST** with a six-hour script timeout and a thin profile wrapper.
- It invokes one `stockctl seed history --market india --market us --deadline 6h --output json --quiet` process. Stockctl owns its checkpointing, work queue, retries, and rate limiting.
- Stdout is silent on success; stockctl emits structured failure status and the wrapper relays only a terminal failure alert. No LLM is used.
- Morning scans only request a delta after the cache’s last bar; they should be near-zero Yahoo traffic before a new completed session exists.

**Acceptance:** CI executes `go test ./...`, a clean checkout compiles embedded data, and the runbook documents resume/reset/inspection commands.

---

## MD-09 — Restore sector mapping datasets

**Objective:** Repair the currently unavailable sector-breadth feature.

**Files:**
- Replace: `internal/marketdata/data/india_sectors.csv`
- Replace: `internal/marketdata/data/us_sectors.csv`
- Create: `internal/marketdata/sectors_test.go`
- Create: `docs/data-sources/sector-mappings.md`

**Implementation:**
- Commit licensed/reproducible mappings, including source date and generation procedure.
- Validate coverage against the embedded universes; emit `Unclassified` explicitly rather than silently conflating it with unknown provider state.

**Acceptance:** sector breadth has useful category coverage for both India and US universes.

---

## Execution order

1. MD-01 and MD-02 — cache correctness first.
2. MD-03 — removes the `scan all` request multiplier.
3. MD-04 and MD-05 — reliable long-running seed and resilient provider handling.
4. MD-06 and MD-07 — trustworthy output/policy.
5. MD-08 and MD-09 — operations and sector completeness.

Do not enable a weekend seeding cron until MD-04 and MD-05 have passing tests. Until then, keep the existing morning automation on one strategy, two workers, and the process lock.
