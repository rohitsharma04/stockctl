# stockctl — Stock Screening CLI

Go CLI for stock screening, pairs trading, and backtesting across **19 global markets** with **1,721 built-in tickers**.

## Quick Reference

```bash
# Build
go build -o stockctl .

# Scan stocks — zero-config, tickers built in for all markets
stockctl scan all --output json                        # US (S&P 500)
stockctl scan breakout-caution -m india --output json   # India (Nifty 500)
stockctl scan all -m japan --output json                # Japan (Nikkei 225)

# With explicit ticker file (overrides built-in)
stockctl scan all --tickers custom.csv -m india --output json

# Inspect a single stock
stockctl inspect AAPL --output json
stockctl inspect RELIANCE -m india --output json

# List built-in universe for a market
stockctl tickers -m germany

# Pairs trading
stockctl pairs --stocks "AAPL,MSFT,GOOGL" --output json

# Backtest
stockctl backtest --input signals.csv

# List all 19 markets
stockctl markets
```

## Tech Stack

- **Language**: Go 1.25
- **CLI**: Cobra
- **Indicators**: `cinar/indicator/v2` (SMA, Bollinger, ATR, HeikinAshi)
- **Market Data**: Yahoo Finance via `oscarli916/yahoo-finance-api`
- **Universes**: Embedded CSV files via `//go:embed` (18 markets, 1,721 tickers)
- **Config**: `~/.config/stockctl/config.toml` (optional — defaults are sensible)

## Conventions

- Always use `--output json` for programmatic results
- Use `-m <id>` — ticker suffix auto-applied (e.g., `RELIANCE` → `RELIANCE.NS`)
- No `--tickers` needed — every market has a built-in universe
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

| Strategy | Signal | Filters |
|---|---|---|
| `breakout-caution` | Bollinger Band breakout + volume + relative strength | 6 |
| `high-performance` | Sustained uptrend with consistent new highs | 8 |
| `stellar-breakout` | Volume explosion + Heikin-Ashi confirmation | 6 |
| `descending-breakout` | Descending triangle breakout with volume | 4 |
| `all` | Run all above | 24 |

## Project Structure

```
cmd/              — Cobra commands (scan, inspect, tickers, pairs, backtest, markets)
internal/
  indicators/     — cinar/indicator v2 wrapper (SMA, Bollinger, ATR, HeikinAshi)
  screener/       — 4 screener strategies with scored FilterResult
  marketdata/     — Yahoo Finance provider, OHLCV types, market definitions
    data/universes/ — Embedded CSV ticker lists for 18 markets
  config/         — TOML config loading
  output/         — JSON, CSV, table formatters
legacy/           — Original Python scripts (reference only)
```
