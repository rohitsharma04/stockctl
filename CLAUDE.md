# stockctl — Stock Screening CLI

Go CLI for stock screening, pairs trading, and backtesting across 19 global markets.

## Quick Reference

```bash
# Build
go build -o stockctl .

# Scan stocks (zero-config: auto-fetches ticker universe)
stockctl scan breakout-caution --market us --output json
stockctl scan all --market india --output json

# With explicit ticker file
stockctl scan all --tickers nifty500.csv --market india --output json

# Fetch/refresh ticker universe
stockctl tickers --market us
stockctl tickers --market india --force

# Inspect a single stock
stockctl inspect AAPL --output json
stockctl inspect RELIANCE --market india --output json

# Pairs trading
stockctl pairs --stocks "AAPL,MSFT,GOOGL" --output json

# Backtest
stockctl backtest --input signals.csv

# List markets
stockctl markets
```

## Tech Stack

- **Language**: Go 1.25
- **CLI**: Cobra
- **Indicators**: `cinar/indicator/v2` (SMA, Bollinger, ATR, etc.)
- **Market Data**: Yahoo Finance via `oscarli916/yahoo-finance-api`
- **Config**: `~/.config/stockctl/config.toml`

## Conventions

- Always use `--output json` for programmatic results
- Use `--market <id>` — ticker suffix auto-applied (e.g., `RELIANCE` → `RELIANCE.NS`)
- Output goes to `/tmp/stockctl/run_<timestamp>/` — never pollutes working directory
- Full command docs: `.agent/skills/stock-analysis/SKILL.md`

## Scan JSON Output

Scan results include per-stock scoring:
```json
[
  {"ticker": "AAPL", "strategy": "breakout-caution", "score": 1.0, "filters_passed": 6, "total_filters": 6}
]
```

## Screener Strategies

| Strategy | Signal |
|---|---|
| `breakout-caution` | Bollinger Band breakout + volume + relative strength |
| `high-performance` | Sustained uptrend with consistent new highs |
| `stellar-breakout` | Volume explosion + Heikin-Ashi confirmation |
| `descending-breakout` | Descending triangle breakout with volume |
| `all` | Run all above |

## Project Structure

```
cmd/           — Cobra commands (scan, pairs, backtest, markets, inspect)
internal/
  indicators/  — cinar/indicator v2 wrapper (SMA, Bollinger, ATR, HeikinAshi)
  screener/    — 4 screener strategies with scored FilterResult
  marketdata/  — Yahoo Finance provider, OHLCV types, market definitions
  config/      — TOML config loading
  output/      — JSON, CSV, table formatters
legacy/        — Original Python scripts (reference only)
```
