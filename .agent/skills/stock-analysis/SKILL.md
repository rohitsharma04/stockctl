---
name: stock-analysis
description: Technical analysis CLI for global stock screening (1,821 tickers, 7 strategies), pairs correlation analysis, and TP/SL strategy backtesting across 19 markets.
---

# Stock Analysis Skill

`stockctl` is a CLI for stock screening, pairs trading, and backtesting across **19 global markets** with **1,821 built-in tickers**. No configuration needed — just pick a market and scan.

## Quick Start

```bash
# Scan US stocks — zero config, tickers are built in
stockctl scan all --output json --quiet

# Scan Indian stocks
stockctl scan breakout-caution -m india --output json --quiet

# Preview a scan without fetching data (dry-run)
stockctl scan all --dry-run --output json --quiet

# Include near-miss stocks (score ≥ 80%)
stockctl scan all --min-score 0.8 --output json --quiet

# Scan as of a past date (historical analysis)
stockctl scan breakout-caution --date 2026-02-03 --output json --quiet

# Scan with per-filter breakdown in results
stockctl scan breakout-caution --detail --output json --quiet

# Deep-analyze a single stock
stockctl inspect AAPL --output json

# Quick price check
stockctl quote AAPL MSFT GOOGL --output json

# Version & capabilities
stockctl version --output json
```

## ⚠️ Execution Rules

> [!CAUTION]
> **Never run multiple `stockctl` commands in parallel.** Always wait for one command to complete before starting the next.

Yahoo Finance enforces rate limits, and `stockctl` shares a disk cache across invocations. Running commands concurrently will trigger API throttling, produce incomplete data, and risk cache corruption.

**Command weight classification:**

| Weight | Commands | Expected Duration | Rule |
|---|---|---|---|
| **Heavy** | `scan all`, `backtest --strategy`, `pairs` | 2–10 min | Always run alone, wait for full completion |
| **Medium** | `scan <single-strategy>`, `inspect` | 30s–4 min | Always run alone, wait for full completion |
| **Light** | `version`, `markets`, `tickers`, `cache`, `quote`, `scan --dry-run` | < 2s | Safe to run individually, but never concurrently with heavy/medium commands |

> [!IMPORTANT]
> Rate limiting is **per-process**. Launching multiple `stockctl` processes defeats the built-in rate limiter and will cause failures.

## Configuration

Config at `~/.stockctl/config.toml` (optional — sensible defaults are built in).
Override with `--config` flag or `STOCKCTL_CONFIG` env var.

Data directory: `~/.stockctl/` (config, cache)

## Market Selection

Every command supports `-m / --market` to target a specific exchange:

```bash
stockctl markets --output json   # List all 19 supported markets
```

The market determines:
- **Ticker suffix** — auto-appended (e.g., `RELIANCE` → `RELIANCE.NS` for India)
- **Benchmark index** — used for relative strength calculations
- **Built-in universe** — tickers embedded in the binary, no download needed
- **Currency** — used in output display

**Important**: Use raw symbols without suffix. The `--market` flag handles suffix application automatically.

## Built-in Ticker Universes

Every market has a pre-loaded ticker universe embedded in the binary. **No `--tickers` flag needed** for supported markets.

| Market | Index | Tickers | Market | Index | Tickers |
|---|---|---|---|---|---|
| `us` | S&P 500 | 503 | `hong-kong` | Hang Seng | 59 |
| `india` | Nifty 500 | 500 | `china` | SSE 50 | 50 |
| `japan` | Nikkei 225 | 100 | `taiwan` | TWSE 50 | 50 |
| `uk` | FTSE 100 | 94 | `australia` | ASX 50 | 50 |
| `brazil` | Ibovespa | 88 | `france` | CAC 40 | 40 |
| `canada` | TSX 60 | 60 | `germany` | DAX 40 | 41 |
| `italy` | FTSE MIB | 39 | `spain` | IBEX 35 | 35 |
| `korea` | KOSPI 50 | 32 | `sweden` | OMX 30 | 30 |
| `singapore` | STI 30 | 30 | `switzerland` | SMI 20 | 20 |

```bash
# List tickers for any market (full list, JSON supported)
stockctl tickers -m japan --output json
stockctl tickers -m uk
```

You can still override with `--tickers custom.csv` for a custom universe.

## Commands

### 1. `stockctl scan <strategy>` — Stock Screening

Scans tickers against technical analysis filters. Runs concurrently with scored results.

> **Duration**: `scan all` → 2–5 min | `scan <strategy>` → 1–3 min (500 tickers). Wait for full completion before running another command.

**Strategies:**

| Strategy | Signal | Key Filters | Filters |
|---|---|---|---|
| `breakout-caution` | Bollinger Band breakout | Price > upper band, volume > 1.5x avg, relative strength > benchmark | 6 |
| `high-performance` | Sustained uptrend | Golden cross, 2x from 52-week low, monotonic SMA200, consistent new highs | 8 |
| `stellar-breakout` | Volume explosion | Weekly volume spike, 61.8% Fibonacci level, Heikin-Ashi bullish confirmation | 6 |
| `descending-breakout` | Chart pattern breakout | Monthly descending triangle trendline break with volume confirmation | 4 |
| `rsi-bounce` | RSI oversold recovery | RSI(14) crossed above 30, RSI in 30–60 range, price > SMA(50), volume spike | 4 |
| `macd-crossover` | MACD bullish crossover | MACD > signal in last 3 days, histogram positive, price > SMA(200), volume ok | 4 |
| `all` | Run all above | — | 32 |

**Usage:**
```bash
# Zero-config — scans built-in S&P 500
stockctl scan breakout-caution --output json --quiet

# Dry-run — preview scan plan without fetching data
stockctl scan all --dry-run --output json --quiet

# Scan Indian NSE — built-in Nifty 500
stockctl scan all -m india --output json --quiet

# Historical analysis — run screener as if it were Feb 3rd
stockctl scan breakout-caution --date 2026-02-03 --output json --quiet

# Include near-miss stocks (score ≥ 80%)
stockctl scan rsi-bounce --min-score 0.8 --output json --quiet

# Include per-filter detail (avoids follow-up inspect calls)
stockctl scan breakout-caution --detail --output json --quiet

# Sort by ticker (default: score descending)
stockctl scan all --sort ticker --output json --quiet

# Set a timeout
stockctl scan all --timeout 3m --output json --quiet
```

**Flags:**
| Flag | Default | Description |
|---|---|---|
| `--date` | (today) | Evaluate as of a past date (`YYYY-MM-DD`). Data is truncated to this date |
| `--tickers <path>` | (built-in universe) | CSV with `Symbol` column or one symbol per line |
| `--market / -m` | `us` | Target market |
| `--workers` | `8` | Concurrent workers |
| `--output / -o` | `table` | `table`, `json`, `csv` |
| `--min-score` | `1.0` | Minimum score to include (0.0–1.0). Use `0.8` for near-misses |
| `--min-price` | from market | Minimum stock price filter |
| `--min-traded-value` | `0` | Minimum 20-day avg traded value (price × volume) for liquidity filtering |
| `--sort` | `score` | Sort results by: `score`, `ticker`, `filters` |
| `--detail` | `false` | Include per-filter breakdown in results |
| `--dry-run` | `false` | Show scan plan without fetching data (instant) |
| `--quiet / -q` | `false` | Suppress all progress output (agent mode) |
| `--no-cache` | `false` | Bypass disk cache |
| `--verbose / -v` | `false` | Show per-ticker errors |

**Output (JSON envelope):**
```json
{
  "meta": {
    "schema_version": "2.0",
    "command": "scan",
    "market": "us",
    "strategy": "breakout-caution",
    "as_of_date": "2026-02-03",
    "tickers_scanned": 503,
    "tickers_failed": 5,
    "duration_ms": 45000,
    "timestamp": "2026-03-26T17:00:00Z",
    "data_quality": {
      "benchmark_available": true,
      "benchmark_symbol": "^GSPC",
      "benchmark_bars": 1258,
      "tickers_complete": 490,
      "tickers_partial": 8,
      "tickers_failed": 5
    }
  },
  "results": {
    "market_summary": {
      "market_id": "us",
      "market_name": "S&P 500",
      "benchmark": { "symbol": "^GSPC", "close": 5200.0, "above_sma50": true, "above_sma200": true, "trend_label": "uptrend" },
      "breadth": { "total_scanned": 503, "full_passes": 12, "near_misses": 35, "pass_rate": 0.024, "above_sma50_pct": 0.65, "regime_label": "broad_risk_on" },
      "sector_breadth": [{ "sector": "Technology", "tickers": 80, "passes": 5, "pass_rate": 0.063 }]
    },
    "results": [
      {
        "ticker": "AAPL", "strategy": "breakout-caution",
        "score": 1.0, "weighted_score": 1.0, "data_confidence": 1.0, "actionability_score": 1.0,
        "filters_passed": 6, "total_filters": 6,
        "close_price": 198.50, "volume": 52340000, "change_pct": 0.012,
        "status": "confirmed_breakout", "status_reason": "",
        "trigger_price": 197.80, "trigger_type": "bollinger_breakout",
        "invalidation_price": 190.50, "atr_stop": 192.30,
        "volume_ratio": 2.1, "required_volume_ratio": 1.5,
        "data_health": "complete",
        "sector": "Technology", "industry": "Technology Hardware, Storage & Peripherals", "cap_tier": "large",
        "timeframe_alignment": "daily+weekly+monthly"
      }
    ]
  },
  "errors": [{"ticker": "XYZ", "error": "no data returned"}],
  "warnings": []
}
```

> **Scoring**: Each result includes `score` (simple pass ratio), `weighted_score` (importance-weighted), `data_confidence` (fraction of filters with valid data), and `actionability_score` (weighted × confidence).
> **Watchlist fields**: `status` (`confirmed_breakout`, `early_breakout`, `watch`, `avoid`), `status_reason`, `trigger_price`, `invalidation_price`, `atr_stop`, `volume_ratio` vs `required_volume_ratio`.
> **Sector enrichment**: `sector`, `industry`, `cap_tier` from embedded classification (500 India tickers, 503 US tickers). `avg_traded_value` for liquidity filtering.
> **Timeframe alignment**: `timeframe_alignment` shows multi-timeframe confirmation (`daily+weekly+monthly`, `daily+weekly`, `daily_only`, `counter_trend`).
> **Market summary**: Top-level `market_summary` with benchmark trend, breadth metrics, sector breakdown, and regime label.
> **Tri-state filters**: Each filter has `status` of `pass`, `fail`, or `unknown`. Unknown filters (missing/NaN data) never count as passes.
> When `--detail` is used, each result also includes a `filters` array with per-filter pass/fail, status, importance, values, and thresholds.

### 2. `stockctl inspect <ticker>` — Single Stock Deep Analysis

Fetches 5 years of daily data and runs all indicators + all 6 screeners with per-filter breakdown.

> **Duration**: 10–30s per ticker. Run one at a time — do not inspect multiple tickers in parallel.

**Usage:**
```bash
stockctl inspect AAPL --output json
stockctl inspect RELIANCE -m india --output json
stockctl inspect 7203 -m japan --output json

# Historical inspection — see indicators/screeners as of a past date
stockctl inspect AAPL --date 2026-02-03 --output json
stockctl inspect RELIANCE -m india --date 2025-12-15 --output json
```

**Output includes:**
- **Price**: close, open, high, low, volume, 52-week high/low
- **Indicators**: SMA(50), SMA(200), Bollinger Bands, ATR(14), Heikin-Ashi, Momentum
- **Screeners**: All 6 strategies with pass/fail, score, and per-filter detail
- **Errored screeners** are included with `"pass": null, "score": null, "error": "..."` instead of being silently omitted

### 3. `stockctl tickers` — List Built-in Universes

Shows the full embedded ticker universe for the active market.

```bash
stockctl tickers --output json         # US (default)
stockctl tickers -m india --output json  # Nifty 500
stockctl tickers -m germany             # DAX 40 (table)
```

### 4. `stockctl pairs` — Pairs Trading Analysis

Finds correlated stock pairs and simulates mean-reversion trading via z-score signals.

> **Duration**: 30s–2 min depending on number of stocks. Always run alone.

**How it works:**
1. Downloads 5 years of daily data for all specified stocks
2. Calculates pairwise Pearson correlation on returns
3. For pairs above threshold, computes OLS hedge ratio
4. Simulates long/short trades: enter at z-score > threshold, exit at reversion

**Usage:**
```bash
stockctl pairs --stocks "AAPL,MSFT,GOOGL,AMZN,META" --output json
stockctl pairs --stocks "RELIANCE,TCS,INFY" -m india --output json
stockctl pairs -m india --threshold 0.6 --z-threshold 1.5
```

**Flags:**
| Flag | Default | Description |
|---|---|---|
| `--stocks` | from config | Comma-separated symbols (raw, suffix auto-applied) |
| `--threshold` | `0.7` | Minimum correlation to form a pair |
| `--z-threshold` | `2.0` | Z-score entry threshold |
| `--window` | `50` | Rolling window for z-score |
| `--capital` | `100000` | Capital per pair |
| `--export-signals` | `false` | Export trade signals as CSV to `/tmp/stockctl/run_*/` |

### 5. `stockctl backtest` — TP/SL Optimization

Grid-searches take-profit and stop-loss combinations for optimal exit parameters.

> **Duration**: 3–10 min. This is the heaviest command — always run alone and wait patiently.

**Two modes:**
```bash
# From CSV file
stockctl backtest --input breakout_signals.csv

# Direct from scan (no CSV needed!)
stockctl backtest --strategy breakout-caution -m us --output json
stockctl backtest --strategy all -m india --output json

# Custom ranges
stockctl backtest --input signals.csv --tp-range 0.10:0.30 --sl-range 0.02:0.08
```

**Strategy metrics** (JSON output includes per-combo diagnostics):
- `sharpe` — risk-adjusted return ratio
- `avg_return`, `win_rate` — basic return statistics
- `avg_win`, `avg_loss` — average winning/losing trade return
- `expectancy` — expected return per trade: `(win_rate × avg_win) + (loss_rate × avg_loss)`
- `max_drawdown` — peak-to-trough drawdown across sequential trades
- `exposure_pct` — fraction of trades that hit TP/SL (vs timing out)
- `total_trades` — number of trades evaluated

### 6. `stockctl cache` — Manage Disk Cache

Market data is cached to `~/.stockctl/cache/` (24-hour TTL) to avoid redundant API calls.

```bash
stockctl cache stats --output json   # View cache size and age
stockctl cache clear                 # Clear all cached data
stockctl cache clear -m india        # Clear only Indian market data
```

### 7. `stockctl markets` — List All Markets

```bash
stockctl markets --output json
```

Shows all 19 markets with Yahoo Finance suffix, benchmark index, currency, and min price.

### 8. `stockctl version` — Version & Capabilities

```bash
stockctl version --output json
```

Returns version, Go version, available strategies, markets, and total ticker count.

### 9. `stockctl quote <tickers...>` — Real-Time Quotes

Fetch current price and volume for one or more tickers.

```bash
stockctl quote AAPL MSFT GOOGL --output json
stockctl quote RELIANCE TCS -m india --output json
```

Bridges the gap between scanning (historical) and execution decisions (current price).

## Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | | `~/.stockctl/config.toml` | Config file path |
| `--market` | `-m` | `us` | Target stock market |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `csv` |
| `--quiet` | `-q` | `false` | Suppress stderr (agent mode) |
| `--no-cache` | | `false` | Bypass disk cache |
| `--verbose` | `-v` | `false` | Show detailed errors |
| `--timeout` | | (none) | Command timeout (e.g., `5m`, `30s`) |
| `--progress` | | `text` | Progress mode: `text`, `json`, `none` (auto `none` with `--quiet`) |

## Output Format

All commands support `--output json` and return a **standard envelope** with schema versioning:

```json
{
  "meta": {"schema_version": "1.0", "command": "...", "market": "...", "duration_ms": 123, "timestamp": "..."},
  "results": ...,
  "errors": [...]
}
```

**Error handling**: Fatal errors (bad market, missing args) also emit the JSON envelope when `--output json` is set, so agents don't need to parse stderr separately.

**Structured progress** (`--progress json`): When enabled, emits NDJSON progress events to stderr:
```jsonl
{"type":"progress","current":250,"total":503,"elapsed_ms":45000}
```

CSV/table exports are written to a **unique per-run temp directory**:
```
/tmp/stockctl/run_20260326_201500_a3f8/
```
Never overwrites files or pollutes the working directory.

## Tips for Agents

1. **Run commands ONE AT A TIME.** Never invoke multiple `stockctl` processes simultaneously — this triggers API rate limits and produces incomplete data. Wait for each command to fully complete before starting the next.
2. **For multi-market analysis**, run each market scan sequentially and aggregate results yourself. Do not parallelize across markets.
3. **Always use `--output json --quiet`** for programmatic consumption (structured envelope, no stderr noise)
4. **Use `--dry-run`** to preview a scan plan (ticker count, strategies, estimated duration) before committing
5. **Use `--detail`** on scan to get per-filter breakdowns — avoids follow-up `inspect` calls for triage
6. **Scan results include `close_price`, `volume`, `change_pct`** — no need to call `inspect` just for price info
7. **Use `--timeout 5m`** to prevent stuck commands from blocking your pipeline
8. **Use `--progress json`** (with or without `--quiet`) to get structured progress events on stderr
9. **Use `--date YYYY-MM-DD`** for historical analysis — run any scan or inspect as if it were a past date
10. **Use `-m <market>`** instead of manually appending suffixes
11. **No `--tickers` needed** — every market has a built-in universe
12. **`scan all` runs all 6 screeners** — stocks appearing in multiple are stronger signals
13. **Use `--min-score 0.8`** to surface near-miss stocks that might pass tomorrow
14. **For Indian stocks**, use `-m india` (NSE) or `-m india-bse` (BSE)
15. **Rate limiting is per-process** — running multiple processes defeats it entirely
16. **Disk cache (24h)** avoids redundant API calls — use `--no-cache` to force refresh
17. **`backtest --strategy`** runs scan→backtest in one step, no CSV intermediary needed
18. **Check `meta.tickers_failed`** in scan output to detect data quality issues
19. **Check `meta.schema_version`** to detect output format changes
20. **Use `version --output json`** to discover available strategies and markets
21. **Use `quote`** for current prices after scanning — bridges historical analysis to live data
22. **Fatal errors emit JSON** when `--output json` is set — no need to parse stderr for errors

## Example: Multi-Step Workflow (Sequential)

Always run commands one at a time. Wait for each to complete before starting the next.

```bash
# Step 0 — Preview the scan plan (instant, no API calls)
stockctl scan all --dry-run -m us --output json --quiet

# Step 1 — Scan US market with timeout (wait 2-5 min for completion)
stockctl scan all -m us --timeout 5m --output json --quiet > /tmp/us_scan.json

# Step 2 — Only after Step 1 completes, scan India
stockctl scan all -m india --timeout 5m --output json --quiet > /tmp/india_scan.json

# Step 3 — Quick price check on candidates (instant)
stockctl quote AAPL MSFT -m us --output json

# Step 4 — Inspect top candidates one at a time (wait 10-30s each)
stockctl inspect AAPL --output json > /tmp/aapl.json

# Step 5 — Backtest (wait 3-10 min)
stockctl backtest --strategy breakout-caution -m us --output json > /tmp/backtest.json
```
