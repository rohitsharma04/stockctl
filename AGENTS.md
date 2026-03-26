# stockctl — Stock Screening CLI

Go CLI for stock screening, pairs trading, and backtesting across 19 global markets.

## Build & Run

```bash
go build -o stockctl .
```

## Key Commands

```bash
# Scan stocks (always use --output json for programmatic results)
stockctl scan <strategy> --tickers stocks.csv --market us --output json
stockctl scan all --market india --tickers nifty500.csv --output json

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

## Important Rules

- **Always use `--output json`** for programmatic consumption
- **Always use `--market <id>`** — ticker suffix is auto-applied (e.g., `RELIANCE` → `RELIANCE.NS`)
- **Never use `cd`** — stockctl can be run from any directory
- **Output files** go to `/tmp/stockctl/run_<timestamp>/` — never pollutes working directory
- **Network required** — stockctl fetches data from Yahoo Finance (will fail in sandboxed environments)

## Strategies

| Strategy | Signal |
|---|---|
| `breakout-caution` | Bollinger Band breakout + volume + relative strength |
| `high-performance` | Sustained uptrend with consistent new highs |
| `stellar-breakout` | Volume explosion + Heikin-Ashi confirmation |
| `descending-breakout` | Descending triangle breakout with volume |
| `all` | Run all above |

## Detailed Docs

See `.agent/skills/stock-analysis/SKILL.md` for complete command documentation, flags, and examples.
