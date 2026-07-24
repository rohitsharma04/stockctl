#!/usr/bin/env python3
"""Refresh the embedded NSE EQ-series universe from NSE's official security list.

Usage (from the repository root):
    python3 scripts/refresh_nse_universe.py

The NSE source is deliberately limited to the EQ series: it represents listed
cash-equity shares that Yahoo Finance exposes with the .NS suffix. It excludes
ETFs, debt, preference shares, and other non-equity instruments.
"""
from __future__ import annotations

import csv
import io
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "internal/marketdata/data/universes/india.csv"
NSE_EQUITY_LIST_URL = "https://archives.nseindia.com/content/equities/EQUITY_L.csv"
TIMEOUT_SECONDS = 60


def official_nse_equities(csv_text: str) -> list[str]:
    """Parse, validate, and sort NSE's official EQ-series symbol list."""
    symbols: set[str] = set()
    for row in csv.DictReader(io.StringIO(csv_text)):
        symbol = row.get("SYMBOL", "").strip()
        series = row.get(" SERIES", row.get("SERIES", "")).strip()
        if series != "EQ":
            continue
        if not symbol:
            raise RuntimeError("NSE EQ source contains an empty SYMBOL")
        if symbol in symbols:
            raise RuntimeError(f"NSE EQ source contains duplicate symbol {symbol!r}")
        symbols.add(symbol)
    if len(symbols) < 2000:
        raise RuntimeError(f"NSE EQ source has {len(symbols)} symbols, want at least 2,000")
    return sorted(symbols)


def download() -> str:
    request = urllib.request.Request(
        NSE_EQUITY_LIST_URL,
        headers={"User-Agent": "stockctl-nse-universe/1.0"},
    )
    with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
        return response.read().decode("utf-8-sig")


def write_universe(symbols: list[str]) -> None:
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    with OUTPUT.open("w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f, lineterminator="\n")
        writer.writerow(["Symbol"])
        writer.writerows((symbol,) for symbol in symbols)
    print(f"wrote {len(symbols)} NSE EQ symbols to {OUTPUT.relative_to(ROOT)}")


def main() -> int:
    write_universe(official_nse_equities(download()))
    return 0


if __name__ == "__main__":
    sys.exit(main())
