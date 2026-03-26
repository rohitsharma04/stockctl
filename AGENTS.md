# stockctl — Stock Screening CLI

Go CLI for stock screening, pairs trading, and backtesting across 19 global markets.

## Build & Run

```bash
go build -o stockctl .
```

## Key Commands

```bash
# Scan stocks — zero-config, 1,721 tickers built into binary
stockctl scan <strategy> --output json               # US (S&P 500, 503 tickers)
stockctl scan all -m india --output json              # India (Nifty 500)
stockctl scan all -m japan --output json              # Japan (Nikkei 225)

# With explicit ticker file (overrides built-in)
stockctl scan all --tickers custom.csv -m india --output json

# Inspect a single stock
stockctl inspect AAPL --output json
stockctl inspect RELIANCE -m india --output json

# List built-in universe
stockctl tickers -m germany

# Pairs trading
stockctl pairs --stocks "AAPL,MSFT,GOOGL" --output json

# Backtest
stockctl backtest --input signals.csv

# List markets
stockctl markets
```

## Important Rules

- **Always use `--output json`** for programmatic consumption
- **Always use `-m <id>`** — ticker suffix is auto-applied (e.g., `RELIANCE` → `RELIANCE.NS`)
- **No `--tickers` needed** — every market has a built-in universe embedded in the binary
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
