#!/usr/bin/env python3
"""Tests for the deterministic, opt-in India universe refresh helpers."""
from __future__ import annotations

import csv
import importlib.util
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("generate_sector_mappings.py")
spec = importlib.util.spec_from_file_location("generate_sector_mappings", SCRIPT)
assert spec and spec.loader
mappings = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mappings)


class IndiaUniverseRefreshTest(unittest.TestCase):
    def test_official_constituents_are_ordered_and_write_deterministically(self) -> None:
        rows = [(f"SYM{i:03d}", "Chemicals") for i in range(500)]
        source = "Company Name,Industry,Symbol\n" + "\n".join(
            f"Company {symbol},{industry},{symbol}" for symbol, industry in rows
        ) + "\n"
        constituents = mappings.official_india_constituents(source)
        self.assertEqual(rows, constituents)

        with tempfile.TemporaryDirectory() as directory:
            original_universes = mappings.UNIVERSES
            original_root = mappings.ROOT
            try:
                mappings.UNIVERSES = Path(directory)
                mappings.ROOT = Path(directory).parent
                mappings.write_india_universe(constituents)
                first = (Path(directory) / "india.csv").read_bytes()
                mappings.write_india_universe(constituents)
                self.assertEqual(first, (Path(directory) / "india.csv").read_bytes())
            finally:
                mappings.UNIVERSES = original_universes
                mappings.ROOT = original_root

        self.assertEqual(
            [symbol for symbol, _ in rows],
            [row["Symbol"] for row in csv.DictReader(first.decode().splitlines())],
        )

    def test_official_constituents_reject_non_500_or_duplicate_symbols(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "want 500"):
            mappings.official_india_constituents("Industry,Symbol\nChemicals,ONE\n")

        source = "Industry,Symbol\n" + "\n".join(
            f"Chemicals,{'DUP' if i >= 498 else f'SYM{i:03d}'}" for i in range(500)
        ) + "\n"
        with self.assertRaisesRegex(RuntimeError, "duplicate"):
            mappings.official_india_constituents(source)


if __name__ == "__main__":
    unittest.main()
