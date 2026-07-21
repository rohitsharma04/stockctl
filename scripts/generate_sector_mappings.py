#!/usr/bin/env python3
"""Regenerate the embedded India and US sector mappings from cited public sources.

Usage (from the repository root):
    python3 scripts/generate_sector_mappings.py
    python3 scripts/generate_sector_mappings.py --refresh-india-universe

Inputs (downloaded with a bounded 60-second timeout):
* India: NSE Indices' official Nifty 500 constituent CSV:
  https://www.niftyindices.com/IndexConstituent/ind_nifty500list.csv
  Its ``Industry`` column is copied verbatim; sector is only a documented,
  deterministic broad grouping of that official industry. The normal command
  regenerates mappings for the embedded universes. --refresh-india-universe is
  the explicit opt-in command that replaces only the embedded India universe
  with the current official 500-symbol constituent list, then maps that exact
  list. It does not write the US universe or US sector mapping.
* US: Wikipedia's S&P 500 constituents table (GICS Sector and GICS
  Sub-Industry), a table which cites S&P Dow Jones Indices as its source:
  https://en.wikipedia.org/wiki/List_of_S%26P_500_companies
  A pinned 2025-12-23 revision is also read solely for companies in the
  repository's older pinned universe. SATS is the former ticker of the current
  table's EchoStar (ECHO) row, so it is an explicit ticker alias, not a guessed
  classification.

Cap tier and category are deliberately ``unknown``/blank when the above sources
have no authoritative value.  No per-company sector or industry is obtained
from Yahoo Finance, and the output is rejected unless it has exact universe
coverage and non-empty Sector and Industry fields.
"""
from __future__ import annotations

import argparse
import csv
import io
import sys
import urllib.request
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/marketdata/data"
UNIVERSES = DATA / "universes"
TIMEOUT_SECONDS = 60
NIFTY500_URL = "https://www.niftyindices.com/IndexConstituent/ind_nifty500list.csv"
SP500_URL = "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies"
# Officially cited historical table revision, retained for the pinned universe.
SP500_2025_12_23_URL = (
    "https://en.wikipedia.org/w/index.php?title=List_of_S%26P_500_companies&oldid=1329075976"
)

# Broad sectors are deterministic taxonomy labels, not company classifications.
INDIA_SECTOR_BY_INDUSTRY = {
    "Automobile and Auto Components": "Consumer Discretionary", "Capital Goods": "Industrials",
    "Chemicals": "Materials", "Construction": "Industrials", "Construction Materials": "Materials",
    "Consumer Durables": "Consumer Discretionary", "Consumer Services": "Consumer Discretionary",
    "Diversified": "Industrials", "Fast Moving Consumer Goods": "Consumer Staples",
    "Financial Services": "Financials", "Healthcare": "Health Care", "Information Technology": "Information Technology",
    "Media Entertainment & Publication": "Communication Services", "Metals & Mining": "Materials",
    "Oil Gas & Consumable Fuels": "Energy", "Power": "Utilities", "Realty": "Real Estate",
    "Services": "Industrials", "Telecommunication": "Communication Services", "Textiles": "Consumer Discretionary",
}


def download(url: str) -> str:
    request = urllib.request.Request(url, headers={"User-Agent": "stockctl-sector-mappings/1.0"})
    with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
        return response.read().decode("utf-8-sig")


def universe(market: str) -> set[str]:
    with (UNIVERSES / f"{market}.csv").open(newline="", encoding="utf-8") as f:
        return {row["Symbol"].strip() for row in csv.DictReader(f) if row["Symbol"].strip()}


def official_india_constituents(csv_text: str) -> list[tuple[str, str]]:
    """Return ordered official Nifty 500 symbols and industries, validating the list."""
    rows = []
    seen = set()
    for row in csv.DictReader(io.StringIO(csv_text)):
        symbol = row["Symbol"].strip()
        industry = row["Industry"].strip()
        if not symbol or not industry:
            raise RuntimeError("Nifty 500 source contains an empty Symbol or Industry")
        if symbol in seen:
            raise RuntimeError(f"Nifty 500 source contains duplicate symbol {symbol!r}")
        seen.add(symbol)
        rows.append((symbol, industry))
    if len(rows) != 500:
        raise RuntimeError(f"Nifty 500 source has {len(rows)} unique symbols, want 500")
    return rows


def write_india_universe(constituents: list[tuple[str, str]]) -> None:
    """Write exactly the official symbols, in official CSV order, to india.csv."""
    output = UNIVERSES / "india.csv"
    with output.open("w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f, lineterminator="\n")
        writer.writerow(["Symbol"])
        writer.writerows((symbol,) for symbol, _ in constituents)
    print(f"wrote {len(constituents)} symbols to {output.relative_to(ROOT)}")


class TableParser(HTMLParser):
    """Small dependency-free HTML table parser for Wikipedia's constituents table."""
    def __init__(self) -> None:
        super().__init__()
        self.tables: list[list[list[str]]] = []
        self.table: list[list[str]] | None = None
        self.row: list[str] | None = None
        self.cell: list[str] | None = None

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag == "table": self.table = []
        elif tag == "tr" and self.table is not None: self.row = []
        elif tag in ("th", "td") and self.row is not None: self.cell = []

    def handle_data(self, data: str) -> None:
        if self.cell is not None: self.cell.append(data)

    def handle_endtag(self, tag: str) -> None:
        if tag in ("th", "td") and self.cell is not None:
            self.row.append("".join(self.cell).strip()); self.cell = None
        elif tag == "tr" and self.row is not None:
            self.table.append(self.row); self.row = None
        elif tag == "table" and self.table is not None:
            self.tables.append(self.table); self.table = None


def sp500_rows(html: str) -> dict[str, tuple[str, str]]:
    parser = TableParser(); parser.feed(html)
    table = next((t for t in parser.tables if t and "Symbol" in t[0] and "GICS Sector" in t[0]), None)
    if table is None: raise RuntimeError("S&P 500 GICS table was not found")
    header = {name: i for i, name in enumerate(table[0])}
    out = {}
    for row in table[1:]:
        if len(row) <= max(header["Symbol"], header["GICS Sector"], header["GICS Sub-Industry"]): continue
        symbol = row[header["Symbol"]].replace(".", "-").strip()
        out[symbol] = (row[header["GICS Sector"]].strip(), row[header["GICS Sub-Industry"]].strip())
    return out


def india_rows(official: dict[str, str] | None = None) -> list[dict[str, str]]:
    if official is None:
        official = dict(official_india_constituents(download(NIFTY500_URL)))
    wanted = universe("india")
    missing = wanted - official.keys()
    if missing: raise RuntimeError(f"India source missing pinned symbols: {sorted(missing)}")
    rows = []
    for symbol in sorted(wanted):
        industry = official[symbol]
        sector = INDIA_SECTOR_BY_INDUSTRY.get(industry)
        if not sector: raise RuntimeError(f"No broad-sector taxonomy for official industry {industry!r}")
        rows.append({"Symbol": symbol, "Sector": sector, "Industry": industry, "CapTier": "unknown", "Category": ""})
    return rows


def us_rows() -> list[dict[str, str]]:
    classifications = sp500_rows(download(SP500_URL))
    classifications.update(sp500_rows(download(SP500_2025_12_23_URL)))
    # EchoStar changed its NASDAQ ticker from SATS to ECHO; retain GICS from its current table row.
    classifications["SATS"] = classifications["ECHO"]
    wanted = universe("us")
    missing = wanted - classifications.keys()
    if missing: raise RuntimeError(f"S&P source missing pinned symbols: {sorted(missing)}")
    return [{"Symbol": s, "Sector": classifications[s][0], "Industry": classifications[s][1], "CapTier": "large", "Category": ""} for s in sorted(wanted)]


def validate(rows: list[dict[str, str]], market: str) -> None:
    wanted = universe(market); actual = [r["Symbol"] for r in rows]
    if len(actual) != len(set(actual)) or set(actual) != wanted:
        raise RuntimeError(f"{market} output does not exactly cover its embedded universe")
    if any(not r["Sector"] or not r["Industry"] for r in rows):
        raise RuntimeError(f"{market} output has an empty Sector or Industry")


def write(market: str, rows: list[dict[str, str]]) -> None:
    validate(rows, market)
    output = DATA / f"{market}_sectors.csv"
    with output.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=["Symbol", "Sector", "Industry", "CapTier", "Category"], lineterminator="\n")
        writer.writeheader(); writer.writerows(rows)
    print(f"wrote {len(rows)} rows to {output.relative_to(ROOT)}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--refresh-india-universe",
        action="store_true",
        help="replace india.csv with the current official Nifty 500 list and regenerate only India mappings",
    )
    args = parser.parse_args()

    if args.refresh_india_universe:
        constituents = official_india_constituents(download(NIFTY500_URL))
        write_india_universe(constituents)
        write("india", india_rows(dict(constituents)))
        return

    write("india", india_rows())
    write("us", us_rows())

if __name__ == "__main__":
    sys.exit(main())
