#!/usr/bin/env python3
"""Tests for the deterministic NSE EQ universe refresh helper."""
from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("refresh_nse_universe.py")
spec = importlib.util.spec_from_file_location("refresh_nse_universe", SCRIPT)
assert spec and spec.loader
nse = importlib.util.module_from_spec(spec)
spec.loader.exec_module(nse)


class NSEUniverseRefreshTest(unittest.TestCase):
    def test_keeps_only_eq_series_and_sorts_symbols(self) -> None:
        rows = [f"SYM{i:04d},EQ" for i in range(2000, 0, -1)]
        rows.extend(["ETFONE,BE", "SMEONE,SM"])
        source = "SYMBOL, SERIES\n" + "\n".join(rows) + "\n"

        symbols = nse.official_nse_equities(source)

        self.assertEqual(2000, len(symbols))
        self.assertEqual("SYM0001", symbols[0])
        self.assertEqual("SYM2000", symbols[-1])
        self.assertNotIn("ETFONE", symbols)

    def test_rejects_duplicate_eq_symbols(self) -> None:
        rows = [f"SYM{i:04d},EQ" for i in range(1, 2000)] + ["SYM0001,EQ"]
        source = "SYMBOL, SERIES\n" + "\n".join(rows) + "\n"
        with self.assertRaisesRegex(RuntimeError, "duplicate"):
            nse.official_nse_equities(source)


if __name__ == "__main__":
    unittest.main()
