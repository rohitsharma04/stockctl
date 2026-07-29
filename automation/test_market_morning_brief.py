"""Regression tests for scheduler-tolerant market brief triggering."""
import datetime as dt
import importlib.util
import json
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


class ScanCommandTests(unittest.TestCase):
    def test_full_nse_scan_has_a_deadline_that_covers_retries_and_rate_limiting(self):
        command = brief.build_scan_command("india", dt.date(2026, 7, 27))

        self.assertEqual(command[0], str(brief.BIN))
        self.assertEqual(command[command.index("--timeout") + 1], "45m")
        self.assertGreaterEqual(brief.SCAN_PROCESS_TIMEOUT_SECONDS, 50 * 60)
        self.assertGreater(brief.SCAN_PROCESS_TIMEOUT_SECONDS, 45 * 60)


class FinalPromptTests(unittest.TestCase):
    def test_embeds_telegram_presentation_contract_and_snapshot(self):
        payload = {
            "market": "india",
            "report_date": "2026-07-24",
            "data_as_of": "2026-07-23",
            "market_summary": {"advance_decline_ratio": 1.2},
            "candidates": [],
        }

        prompt = brief.final_prompt(payload)

        self.assertIn("🇮🇳 **NSE Pre-Open Brief — <report date>**", prompt)
        self.assertIn("🇺🇸 **US Pre-Open Brief — <report date>**", prompt)
        self.assertIn("prior-close/non-live", prompt)
        self.assertIn("snapshot's `data_as_of` date", prompt)
        self.assertIn("🔴 RISK-OFF", prompt)
        self.assertIn("🟢 RISK-ON", prompt)
        self.assertIn("🟡 NEUTRAL", prompt)
        self.assertIn("exactly one sentence", prompt)
        self.assertIn("🚨 **What matters now**", prompt)
        self.assertIn("No actionable setups today", prompt)
        self.assertIn("❗", prompt)
        self.assertIn("📊 **Market internals**", prompt)
        self.assertIn("2–4 compact bullets", prompt)
        self.assertIn("🎯 **Watchlist / candidates**", prompt)
        self.assertIn("at most three candidates", prompt)
        self.assertIn("exactly two short lines", prompt)
        self.assertIn("🔎 **Next check**", prompt)
        self.assertIn("180–260 words maximum", prompt)
        self.assertIn("Do not use `##` headers, box-drawing characters, dense tables", prompt)
        self.assertIn(json.dumps(payload, separators=(",", ":"), ensure_ascii=False), prompt)


if __name__ == "__main__":
    unittest.main()
