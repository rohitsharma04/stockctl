"""Regression tests for scheduler-tolerant market brief triggering."""
import datetime as dt
import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("market_morning_brief.py")
SPEC = importlib.util.spec_from_file_location("market_morning_brief", MODULE_PATH)
brief = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(brief)


class DueMarketTests(unittest.TestCase):
    def test_india_brief_runs_when_scheduler_starts_a_few_minutes_late(self):
        now = dt.datetime(2026, 7, 23, 3, 3, tzinfo=dt.timezone.utc)  # 08:33 IST
        self.assertEqual(brief.due_market(now), "india")


if __name__ == "__main__":
    unittest.main()
