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

**Strategies:** `breakout-caution`, `high-performance`, `stellar-breakout`, `descending-breakout`, `rsi-bounce`, `macd-crossover`, `all`

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

## Automated morning briefs

`automation/market_morning_brief.py` is designed for a scheduler: it checks the
actual NSE/NYSE trading calendar, holds a process-wide lock so there is never more
than one `stockctl` process, runs a **single** rate-limited scan (`--workers 2`),
and calls Hermes only after the quality gate passes to turn the result into the
final Telegram update.

```bash
# One-time setup
python3.11 -m venv .venv
.venv/bin/pip install -r requirements-market-brief.txt
go build -o bin/stockctl .

# Exercise a collector without calling the final agent
STOCKCTL_BRIEF_FORCE_MARKET=india STOCKCTL_BRIEF_DRY_RUN=1 \
  .venv/bin/python automation/market_morning_brief.py
```

Normal scheduled behavior:

| Market | Trigger | Calendar guard |
|---|---:|---|
| India / NSE | 08:30 IST | NSE trading session |
| US / NYSE | 08:00 America/New_York (30 minutes before the regular open) | NYSE trading session + DST-aware local time |

The automation stores raw scan JSON and the compact agent input under
`/tmp/stockctl/market-briefs/`. It rejects a scan with a missing benchmark, fewer
than 400 scanned symbols, or excessive fetch failures; in that case it sends
nothing rather than publishing a misleading update.

## Resumable weekend history seed

`stockctl seed history` populates and refreshes the local daily-history cache.
It owns its checkpoint, bounded retry/backoff, rate limit, and resume semantics;
Hermes should only schedule one long-running process and report a terminal failure.

```bash
go build -o bin/stockctl .
bin/stockctl --quiet --output json seed history \
  --market india --market us \
  --period max \
  --rate 1 --workers 1 --max-attempts 3 --deadline 6h \
  --state-file ~/.stockctl/seed-history-state.json
```

The command is sequential by design (`--workers 1`) to protect Yahoo rate limits.
It emits a single JSON envelope to stdout (`results.summary` and `results.failures`). A nonzero exit means there are terminal
failures or work still pending after the deadline; rerun the same command to resume
without re-fetching successful tickers for the same history period and coverage
intent. Checkpoints record both; changing from the legacy implicit `5y` seed to
`--period max` resets ticker work so full-history downloads are not skipped. See
[`docs/operations/yahoo-data-runbook.md`](docs/operations/yahoo-data-runbook.md)
for checkpoint inspection, recovery, and the Hermes-launcher contract. Inspect safely with:

```bash
bin/stockctl --output json seed status --state-file ~/.stockctl/seed-history-state.json
bin/stockctl --output json seed verify --market india --market us --period max
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
automation/     Calendar-aware no-agent market collector
config.toml     Default configuration template
.agent/         AI agent skill + workflow definitions
```

## License

MIT
