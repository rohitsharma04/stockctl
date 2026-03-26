---
name: stock-analysis
description: Scan stocks for breakout/momentum signals, run pairs trading analysis, and backtest trading strategies using the stockctl CLI
---

# Stock Analysis Skill

`stockctl` is a CLI for stock screening, pairs trading, and backtesting. It supports 19 global stock markets.

## Configuration

Config lives at `~/.config/stockctl/config.toml` (override with `STOCKCTL_CONFIG` env var or `--config` flag).

## Market Selection

Every command supports a `--market` flag to target a specific exchange. The market determines:
- **Ticker suffix** — auto-appended to raw symbols (e.g., `RELIANCE` → `RELIANCE.NS` for India)
- **Benchmark index** — used for relative strength calculations
- **Currency** — used in output display

```bash
# List all supported markets
stockctl markets
```

Key markets: `us` (default), `india`, `japan`, `uk`, `germany`, `hong-kong`, `china`, `canada`, `australia`, `korea`

**Important**: When `--market` is set, raw ticker symbols (without suffix) are expected. The suffix is applied automatically. If tickers already contain the correct suffix, they won't be double-suffixed.

## Commands

### 1. `stockctl scan <strategy>` — Stock Screening

Scans a universe of tickers against technical analysis filters. Runs concurrently.

**Strategies:**

| Strategy | Signal | Key Filters |
|---|---|---|
| `breakout-caution` | Bollinger Band breakout | Price > upper band, volume > 1.5x avg, relative strength > benchmark |
| `high-performance` | Sustained uptrend | Golden cross, 2x from 52-week low, monotonic SMA200, consistent new highs |
| `stellar-breakout` | Volume explosion | Weekly volume spike, 61.8% Fibonacci level, Heikin-Ashi bullish confirmation |
| `descending-breakout` | Chart pattern breakout | Monthly descending triangle trendline break with volume confirmation |
| `all` | Run all above | — |

**Usage:**
```bash
# Scan US stocks (default market)
stockctl scan breakout-caution --tickers stocks.csv

# Scan Indian NSE stocks
stockctl scan all --market india --tickers nifty500.csv

# JSON output for programmatic use
stockctl scan high-performance --tickers stocks.csv --output json

# Increase concurrency
stockctl scan all --tickers stocks.csv --workers 16
```

**Flags:**
- `--tickers <path>` — CSV file with a `Symbol` or `Ticker` column (or one symbol per line)
- `--market <id>` — Target market (default: from config)
- `--workers <n>` — Concurrent workers (default: 8)
- `--output <format>` — `table` (default), `json`, `csv`
- `--verbose` — Show per-ticker errors

**Output (JSON):**
```json
[
  {"ticker": "AAPL", "strategy": "breakout-caution"},
  {"ticker": "NVDA", "strategy": "breakout-caution"}
]
```

### 2. `stockctl pairs` — Pairs Trading Analysis

Finds highly correlated stock pairs and simulates mean-reversion trading using z-score signals.

**How it works:**
1. Downloads 5 years of daily data for all specified stocks
2. Calculates pairwise Pearson correlation on returns
3. For pairs above the correlation threshold, computes OLS hedge ratio
4. Simulates long/short trades: enter when z-score > threshold, exit when z-score reverts to 0

**Usage:**
```bash
# Indian F&O stocks (default from config)
stockctl pairs --market india

# Custom stock list
stockctl pairs --stocks "AAPL,MSFT,GOOGL,AMZN,META"

# Adjust sensitivity
stockctl pairs --stocks "RELIANCE,TCS,INFY" --market india --threshold 0.6 --z-threshold 1.5
```

**Flags:**
- `--stocks "SYM1,SYM2,..."` — Comma-separated symbols (raw, suffix auto-applied)
- `--threshold <float>` — Minimum correlation to form a pair (default: 0.7)
- `--z-threshold <float>` — Z-score entry threshold (default: 2.0)
- `--window <int>` — Rolling window for z-score (default: 50)
- `--capital <float>` — Capital per pair

**Output includes:** Correlation coefficient, hedge ratio, trade count, P&L, win rate for each pair.

### 3. `stockctl backtest` — TP/SL Optimization

Grid-searches take-profit and stop-loss combinations to find optimal exit parameters.

**Usage:**
```bash
# Default ranges from config
stockctl backtest --input breakout_signals.csv

# Custom TP/SL ranges
stockctl backtest --input signals.csv --tp-range 0.10:0.30 --sl-range 0.02:0.08
```

**Input CSV format:**
```
symbol,entry_date,entry_price,highs,lows,closes
AAPL,2024-06-15,185.50,"[186,188,190]","[184,183,185]","[185,187,189]"
```

**Flags:**
- `--input <path>` — CSV with breakout entries
- `--tp-range min:max` — Take-profit range (default: 5%–50%)
- `--sl-range min:max` — Stop-loss range (default: 1%–10%)
- `--capital <float>` — Capital per trade

**Output includes:** Top 10 TP/SL combos ranked by return, Sharpe ratio, win rate per strategy.

### 4. `stockctl markets` — List Supported Markets

```bash
stockctl markets
```

Shows all 19 supported markets with their Yahoo Finance suffix, benchmark index, and currency.

## Global Flags

These apply to all commands:

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | | `~/.config/stockctl/config.toml` | Config file path |
| `--market` | `-m` | `us` | Target stock market |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `csv` |
| `--verbose` | `-v` | `false` | Show detailed errors |

## Output Directory

All output files (CSV exports, etc.) are written to a **unique per-run temporary directory**:

```
/tmp/stockctl/run_20260326_201500_a3f8/
```

- Each run creates a new directory with timestamp + random suffix — **files are never overwritten**
- The path is printed to stderr when CSV output is used
- The command itself can be run from any directory; it will never pollute the current working directory

## Ticker CSV Format

The `--tickers` file can be:
- **CSV with header**: Must have a `Symbol` or `Ticker` column
- **Plain text**: One ticker per line

Tickers should be **raw symbols without market suffix** (e.g., `RELIANCE` not `RELIANCE.NS`). The `--market` flag handles suffix application.

## Tips for Agents

1. **Always use `--output json`** for programmatic consumption of scan/pairs results
2. **Use `--market`** instead of manually appending suffixes to tickers
3. **The `scan all` command** runs all 4 screeners — stocks appearing in multiple are stronger signals
4. **For Indian stocks**, use `--market india` (NSE) or `--market india-bse` (BSE)
5. **Rate limiting is built-in** — no need to add delays between calls
6. **Output files go to `/tmp/stockctl/run_<ts>_<id>/`** — never pollutes the working directory
7. **Can be run from any directory** — no need to `cd` anywhere first

