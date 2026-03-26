---
name: stock-analysis
description: Scan stocks for breakout/momentum signals, run pairs trading analysis, and backtest trading strategies using the stockctl CLI
---

# Stock Analysis Skill

`stockctl` is a CLI for stock screening, pairs trading, and backtesting across **19 global markets** with **1,721 built-in tickers**. No configuration needed — just pick a market and scan.

## Quick Start

```bash
# Scan US stocks — zero config, tickers are built in
stockctl scan all --output json

# Scan Indian stocks
stockctl scan breakout-caution -m india --output json

# Deep-analyze a single stock
stockctl inspect AAPL --output json

# Inspect an Indian stock
stockctl inspect RELIANCE -m india --output json
```

## Configuration

Config at `~/.config/stockctl/config.toml` (optional — sensible defaults are built in).
Override with `--config` flag or `STOCKCTL_CONFIG` env var.

## Market Selection

Every command supports `-m / --market` to target a specific exchange:

```bash
stockctl markets   # List all 19 supported markets
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
# List tickers for any market
stockctl tickers -m japan
stockctl tickers -m uk
```

You can still override with `--tickers custom.csv` for a custom universe.

## Commands

### 1. `stockctl scan <strategy>` — Stock Screening

Scans tickers against technical analysis filters. Runs concurrently with scored results.

**Strategies:**

| Strategy | Signal | Key Filters | Filters |
|---|---|---|---|
| `breakout-caution` | Bollinger Band breakout | Price > upper band, volume > 1.5x avg, relative strength > benchmark | 6 |
| `high-performance` | Sustained uptrend | Golden cross, 2x from 52-week low, monotonic SMA200, consistent new highs | 8 |
| `stellar-breakout` | Volume explosion | Weekly volume spike, 61.8% Fibonacci level, Heikin-Ashi bullish confirmation | 6 |
| `descending-breakout` | Chart pattern breakout | Monthly descending triangle trendline break with volume confirmation | 4 |
| `all` | Run all above | — | 24 |

**Usage:**
```bash
# Zero-config — scans built-in S&P 500
stockctl scan breakout-caution --output json

# Scan Indian NSE — built-in Nifty 500
stockctl scan all -m india --output json

# Scan Japanese market
stockctl scan high-performance -m japan --output json

# Custom ticker file
stockctl scan stellar-breakout --tickers custom.csv --output json

# Increase concurrency
stockctl scan all --workers 16 --output json
```

**Flags:**
| Flag | Default | Description |
|---|---|---|
| `--tickers <path>` | (built-in universe) | CSV with `Symbol` column or one symbol per line |
| `--market / -m` | `us` | Target market |
| `--workers` | `8` | Concurrent workers |
| `--output / -o` | `table` | `table`, `json`, `csv` |
| `--min-price` | from market | Minimum stock price filter |
| `--verbose / -v` | `false` | Show per-ticker errors |

**Output (JSON):**
```json
[
  {"ticker": "AAPL", "strategy": "breakout-caution", "score": 1.0, "filters_passed": 6, "total_filters": 6},
  {"ticker": "NVDA", "strategy": "breakout-caution", "score": 1.0, "filters_passed": 6, "total_filters": 6}
]
```

Each result includes `score` (0.0–1.0 = filters_passed / total_filters). A stock passes only when score = 1.0.

### 2. `stockctl inspect <ticker>` — Single Stock Deep Analysis

Fetches 5 years of daily data and runs all indicators + all 4 screeners with per-filter breakdown.

**Usage:**
```bash
stockctl inspect AAPL --output json
stockctl inspect RELIANCE -m india --output json
stockctl inspect 7203 -m japan --output json
```

**Output includes:**
- **Price**: close, open, high, low, volume, 52-week high/low
- **Indicators**: SMA(50), SMA(200), Bollinger Bands, ATR(14), Heikin-Ashi, Momentum
- **Screeners**: All 4 strategies with pass/fail, score, and per-filter detail

**JSON output (excerpt):**
```json
{
  "ticker": "AAPL",
  "price": {"close": 256.11, "high_52w": 286.19, "low_52w": 172.42},
  "indicators": {"sma_50": 260.57, "sma_200": 247.84, "bollinger_upper": 267.03},
  "screeners": [
    {
      "name": "breakout-caution", "pass": false, "score": 0.5,
      "filters": [
        {"name": "min_price", "pass": true, "detail": "$256.11"},
        {"name": "bollinger_breakout", "pass": false, "detail": "high 257.00 vs upper band 267.03"}
      ]
    }
  ]
}
```

### 3. `stockctl tickers` — List Built-in Universes

Shows the embedded ticker universe for the active market.

```bash
stockctl tickers             # US (default)
stockctl tickers -m india    # Nifty 500
stockctl tickers -m germany  # DAX 40
```

### 4. `stockctl pairs` — Pairs Trading Analysis

Finds correlated stock pairs and simulates mean-reversion trading via z-score signals.

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

### 5. `stockctl backtest` — TP/SL Optimization

Grid-searches take-profit and stop-loss combinations for optimal exit parameters.

```bash
stockctl backtest --input breakout_signals.csv
stockctl backtest --input signals.csv --tp-range 0.10:0.30 --sl-range 0.02:0.08
```

### 6. `stockctl markets` — List All Markets

```bash
stockctl markets
```

Shows all 19 markets with Yahoo Finance suffix, benchmark index, currency, and min price.

## Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | | `~/.config/stockctl/config.toml` | Config file path |
| `--market` | `-m` | `us` | Target stock market |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `csv` |
| `--verbose` | `-v` | `false` | Show detailed errors |

## Output Directory

CSV/table exports are written to a **unique per-run temp directory**:
```
/tmp/stockctl/run_20260326_201500_a3f8/
```
Never overwrites files or pollutes the working directory.

## Tips for Agents

1. **Always use `--output json`** for programmatic consumption
2. **Use `-m <market>`** instead of manually appending suffixes
3. **No `--tickers` needed** — every market has a built-in universe
4. **`scan all` runs all 4 screeners** — stocks appearing in multiple are stronger signals
5. **For Indian stocks**, use `-m india` (NSE) or `-m india-bse` (BSE)
6. **Rate limiting is built-in** — no need to add delays between calls
7. **Can be run from any directory** — no need to `cd` anywhere
8. **Scored results**: `score` (0–1) and `filters_passed`/`total_filters` rank near-misses
9. **Cross-strategy analysis**: Compare scores across strategies to find strongest setups
10. **Use `inspect` first** for single-stock deep analysis before running broad scans
11. **Output files** go to `/tmp/stockctl/run_<ts>_<id>/` — check stderr for path

## Cross-References

- `CLAUDE.md` (Claude Code) — auto-read at session start
- `AGENTS.md` (Codex) — read before every task
- `.agent/skills/stock-analysis/SKILL.md` (Gemini CLI) — this file
