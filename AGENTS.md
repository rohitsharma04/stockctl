# stockctl — Developer Guidance

This document provides essential context for AI agents and developers modifying the `stockctl` codebase. For information on how to **use** the CLI, see [.agent/skills/stock-analysis/SKILL.md](file:///.agent/skills/stock-analysis/SKILL.md).

## Tech Stack

- **Language**: Go 1.25+
- **CLI Framework**: [Cobra](https://github.com/spf13/cobra)
- **Configuration**: TOML via `~/.stockctl/config.toml`
- **Market Data**: Yahoo Finance API via `oscarli916/yahoo-finance-api`
- **Technical Indicators**: `cinar/indicator/v2` wrapper in `internal/indicators`
- **Concurrency**: Parallel processing for scans and backtests

## Project Structure

- `cmd/`: CLI command definitions (scan, inspect, pairs, backtest, etc.).
- `internal/`:
    - `marketdata/`: Data providers, market registry, and embedded ticker universes (`data/universes/`).
    - `screener/`: Implementation of technical screening strategies.
    - `indicators/`: Wrapper for technical indicators and relative strength logic.
    - `pairs/`: Logic for correlation analysis and pairs trading simulation.
    - `backtest/`: TP/SL optimization engine.
    - `output/`: Formatters for JSON, CSV, and Table output.
    - `config/`: TOML configuration management.
- `legacy/`: Archived Python scripts for reference.

## Build & Test

```bash
# Build the binary
go build -o stockctl .

# Run tests
go test ./...
```

## Coding Conventions

- **Structured Output**: All commands must support `--output json` and return the standard JSON envelope defined in `internal/output`.
- **Market Abstraction**: Use the `Market` registry in `internal/marketdata` to handle ticker suffixes and benchmark indices.
- **Concurrency**: Use worker pools (see `internal/screener`) for high-throughput scanning.
- **Embedded Data**: Ticker universes are embedded using `//go:embed`. Update `internal/marketdata/data/universes/` to add/modify markets.
- **Clean CLI**: Suppress non-essential output to stderr when `--quiet` is set.

## Critical Rules

- **Never use `cd`**: The tool and tests should be runnable from the root.
- **No Overwrites**: Output files should go to `/tmp/stockctl/run_<timestamp>/` to avoid polluting the workspace.
- **Agent Mode**: Always prioritize JSON output for programmatic interaction.
