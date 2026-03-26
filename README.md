# stockctl

A CLI for stock screening, pairs trading, and backtesting. Supports 19 global markets via Yahoo Finance.

## Install

```bash
go install github.com/rohitsharma04/stockctl@latest
```

Or build from source:

```bash
go build -o stockctl .
sudo mv stockctl /usr/local/bin/
```

## Setup

Create your config file:

```bash
mkdir -p ~/.stockctl
cp config.toml ~/.stockctl/config.toml
```

Edit `~/.stockctl/config.toml` to set your default market and strategy thresholds.

Override path: set `STOCKCTL_CONFIG` env var or use `--config` flag.

## Usage

### Screen stocks for breakout signals

```bash
stockctl scan breakout-caution --tickers stocks.csv --market india
stockctl scan all --tickers nifty500.csv --output json
```

**Strategies:** `breakout-caution`, `high-performance`, `stellar-breakout`, `descending-breakout`, `all`

### Pairs trading analysis

```bash
stockctl pairs --stocks "RELIANCE,TCS,HDFCBANK,INFY" --market india
stockctl pairs --stocks "AAPL,MSFT,GOOGL,AMZN" --threshold 0.7
```

### Backtest TP/SL optimization

```bash
stockctl backtest --input breakout_signals.csv
stockctl backtest --input signals.csv --tp-range 0.10:0.30 --sl-range 0.02:0.05
```

### List supported markets

```bash
stockctl markets
```

## Markets

19 exchanges supported — US, India (NSE/BSE), Japan, UK, Germany, France, Canada, Australia, Hong Kong, China, South Korea, Singapore, Brazil, Taiwan, Italy, Spain, Sweden, Switzerland.

The `--market` flag auto-appends the correct Yahoo Finance suffix to raw tickers (e.g., `RELIANCE` → `RELIANCE.NS` for `--market india`).

## Output

- `--output table` — terminal table (default)
- `--output json` — structured JSON envelope to stdout (`{meta, results, errors}`)
- `--output csv` — writes to `/tmp/stockctl/run_<timestamp>_<id>/`

Each run creates a unique output directory — files never overwrite.

### Agent-friendly flags

- `--quiet` / `-q` — suppress all progress output (agent mode)
- `--min-score 0.8` — include near-miss stocks (default: 1.0 = only full passes)

## Agent Integration

This repo includes an agent skill (`.agent/skills/stock-analysis/SKILL.md`) for use with Claude Code, Gemini CLI, and similar AI agents.

**Slash commands:**
- `/scan-breakouts` — run stock screeners
- `/pairs-trading` — run pairs trading analysis
- `/backtest-strategy` — run TP/SL optimization

## Project Structure

```
cmd/            CLI commands (scan, pairs, backtest, markets)
internal/
  config/       TOML config parser
  marketdata/   Yahoo Finance provider, market registry, types
  indicators/   SMA, Bollinger, ATR, Heikin-Ashi, Relative Strength
  screener/     4 screening strategies
  pairs/        Correlation, hedge ratio, trading simulator
  backtest/     Parallel TP/SL grid search
  output/       Table, JSON, CSV formatters
config.toml     Default configuration template
.agent/         AI agent skill + workflow definitions
legacy/         Original Python scripts (archived)
```

## License

MIT
