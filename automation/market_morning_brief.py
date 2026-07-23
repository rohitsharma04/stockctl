#!/usr/bin/env python3
"""No-agent market collector with an agent used only to write the final update.

The cron scheduler can invoke this file frequently.  It exits silently unless it
is exactly the intended local market time and the relevant exchange is open.
One process-wide lock protects Yahoo's per-process limiter and disk cache from
concurrent stockctl runs.
"""
from __future__ import annotations

import datetime as dt
import fcntl
import json
import os
import subprocess
import sys
from pathlib import Path
from zoneinfo import ZoneInfo

import pandas_market_calendars as mcal

ROOT = Path(__file__).resolve().parents[1]
BIN = Path(os.environ.get("STOCKCTL_BIN", ROOT / "bin" / "stockctl"))
STATE_ROOT = Path(os.environ.get("STOCKCTL_BRIEF_STATE", "/tmp/stockctl/market-briefs"))
LOCK_PATH = Path("/tmp/stockctl/market-brief.lock")

MARKETS = {
    "india": {
        "calendar": "NSE",
        "tz": "Asia/Kolkata",
        "hour": 8,
        "minute": 30,
        "label": "India / NSE",
    },
    "us": {
        "calendar": "NYSE",
        "tz": "America/New_York",
        "hour": 8,
        "minute": 0,
        "label": "US / NYSE",
    },
}


def log(message: str) -> None:
    print(f"market-brief: {message}", file=sys.stderr)


def market_is_open(market: str, local_day: dt.date) -> bool:
    schedule = mcal.get_calendar(MARKETS[market]["calendar"]).schedule(
        start_date=local_day, end_date=local_day
    )
    return not schedule.empty


def previous_completed_session(market: str, report_day: dt.date) -> dt.date:
    """Return the last exchange session before a pre-open report day.

    We pass this explicitly to stockctl rather than allowing its current-time
    default to label a pre-open daily-bar report as today's close.
    """
    calendar = mcal.get_calendar(MARKETS[market]["calendar"])
    schedule = calendar.schedule(
        start_date=report_day - dt.timedelta(days=14),
        end_date=report_day - dt.timedelta(days=1),
    )
    if schedule.empty:
        raise RuntimeError(f"no prior {MARKETS[market]['calendar']} session before {report_day}")
    return schedule.index[-1].date()


def due_market(now_utc: dt.datetime) -> str | None:
    forced = os.getenv("STOCKCTL_BRIEF_FORCE_MARKET", "").lower()
    if forced:
        if forced not in MARKETS:
            raise ValueError("STOCKCTL_BRIEF_FORCE_MARKET must be india or us")
        return forced

    for market, cfg in MARKETS.items():
        local_now = now_utc.astimezone(ZoneInfo(cfg["tz"]))
        # Cron dispatch can start a few minutes after its nominal minute. The
        # per-market sent marker below makes this grace window idempotent.
        if (
            local_now.hour == cfg["hour"]
            and cfg["minute"] <= local_now.minute < cfg["minute"] + 15
        ):
            return market
    return None


def compact_payload(
    scan: dict, market: str, local_now: dt.datetime, data_as_of: dt.date
) -> dict:
    meta = scan.get("meta", {})
    results = scan.get("results", {})
    summary = results.get("market_summary") or {}
    candidates = results.get("results") or []
    ranked = sorted(
        candidates,
        key=lambda item: (item.get("actionability_score", 0), item.get("weighted_score", 0)),
        reverse=True,
    )[:10]
    keep = (
        "ticker", "sector", "close_price", "change_pct", "status", "status_reason",
        "actionability_score", "weighted_score", "trigger_price", "invalidation_price",
        "atr_stop", "volume_ratio", "required_volume_ratio", "data_health",
    )
    return {
        "market": market,
        "market_label": MARKETS[market]["label"],
        "generated_at": local_now.isoformat(),
        "report_date": local_now.date().isoformat(),
        "data_as_of": data_as_of.isoformat(),
        "provider_as_of_label": summary.get("as_of_date") or meta.get("as_of_date"),
        "scan": {
            "strategy": meta.get("strategy"),
            "tickers_scanned": meta.get("tickers_scanned"),
            "tickers_failed": meta.get("tickers_failed"),
            "duration_ms": meta.get("duration_ms"),
            "data_quality": meta.get("data_quality"),
            "warnings": scan.get("warnings", []),
        },
        "market_summary": summary,
        "candidates": [{k: c.get(k) for k in keep if k in c} for c in ranked],
    }


def scan_is_usable(scan: dict) -> tuple[bool, str]:
    meta = scan.get("meta", {})
    quality = meta.get("data_quality") or {}
    scanned = int(meta.get("tickers_scanned") or 0)
    failed = int(meta.get("tickers_failed") or 0)
    partial = int(quality.get("tickers_partial") or 0)
    stale = int(quality.get("stale_tickers") or 0)
    if scanned < 400:
        return False, f"only {scanned} tickers scanned"
    if failed > max(25, int(scanned * 0.15)):
        return False, f"{failed}/{scanned} ticker fetches failed"
    if partial > max(50, int(scanned * 0.20)):
        return False, f"{partial}/{scanned} ticker fetches partial"
    if stale > max(25, int(scanned * 0.10)):
        return False, f"{stale}/{scanned} ticker stale fallbacks"
    if not quality.get("benchmark_available"):
        return False, "benchmark unavailable"
    expected_as_of = str(scan.get("meta", {}).get("as_of_date") or "")
    if expected_as_of and quality.get("data_as_of") and quality["data_as_of"] < expected_as_of:
        return False, f"data only available as of {quality['data_as_of']}, expected {expected_as_of}"
    return True, ""


def final_prompt(payload: dict) -> str:
    return f"""You are writing the final Telegram update from a completed automated market scan.\n\nRules:\n- Use ONLY this snapshot; do not call tools, browse, fetch quotes, or invent facts.\n- Keep it concise and decision-useful: market regime/breadth first, then at most five candidate tickers.\n- State the report's `data_as_of` date prominently: this is a prior-close daily-bar report, not live execution pricing.\n- Use clear Telegram Markdown. Do not give personalised investment advice or claim certainty.\n- If no candidates passed, say so plainly and highlight breadth/regime instead.\n\nSnapshot:\n{json.dumps(payload, separators=(',', ':'), ensure_ascii=False)}\n"""


def main() -> int:
    now_utc = dt.datetime.now(dt.timezone.utc)
    try:
        market = due_market(now_utc)
    except ValueError as exc:
        log(str(exc))
        return 2
    if not market:
        return 0

    cfg = MARKETS[market]
    local_now = now_utc.astimezone(ZoneInfo(cfg["tz"]))
    if not market_is_open(market, local_now.date()):
        log(f"{market} closed on {local_now.date()}; skipped")
        return 0
    if not BIN.is_file() or not os.access(BIN, os.X_OK):
        log(f"stockctl executable missing: {BIN}; run `go build -o bin/stockctl .`")
        return 1
    try:
        data_as_of = previous_completed_session(market, local_now.date())
    except RuntimeError as exc:
        log(str(exc))
        return 1

    LOCK_PATH.parent.mkdir(parents=True, exist_ok=True)
    with LOCK_PATH.open("w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            log("another stockctl market brief is running; skipped")
            return 0

        run_dir = STATE_ROOT / f"run_{local_now:%Y%m%d}_{market}"
        run_dir.mkdir(parents=True, exist_ok=True)
        raw_path = run_dir / "scan.json"
        payload_path = run_dir / "brief-input.json"
        sent_path = run_dir / "sent.txt"

        if sent_path.exists() and not os.getenv("STOCKCTL_BRIEF_FORCE_MARKET"):
            return 0

        command = [
            str(BIN), "scan", "breakout-caution", "--market", market,
            "--date", data_as_of.isoformat(), "--workers", "2", "--min-score", "0.67", "--detail",
            "--timeout", "4m", "--output", "json", "--quiet",
        ]
        log("starting a single rate-limited scan: " + " ".join(command[1:]))
        result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, timeout=270)
        if result.returncode != 0:
            log(f"stockctl failed (exit {result.returncode}): {result.stderr[-800:]}")
            return result.returncode or 1
        try:
            scan = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            log(f"stockctl emitted invalid JSON: {exc}")
            return 1
        usable, reason = scan_is_usable(scan)
        if not usable:
            log(f"scan quality gate rejected output: {reason}")
            return 1

        raw_path.write_text(json.dumps(scan, indent=2) + "\n")
        payload = compact_payload(scan, market, local_now, data_as_of)
        payload_path.write_text(json.dumps(payload, indent=2) + "\n")

        if os.getenv("STOCKCTL_BRIEF_DRY_RUN"):
            print(json.dumps(payload, indent=2))
            return 0

        agent = subprocess.run(
            ["hermes", "--profile", "rohit", "chat", "-Q", "--toolsets", "safe", "-q", final_prompt(payload)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            timeout=90,
        )
        if agent.returncode != 0 or not agent.stdout.strip():
            log(f"final update agent failed (exit {agent.returncode}): {agent.stderr[-800:]}")
            return agent.returncode or 1
        sent_path.write_text(now_utc.isoformat() + "\n")
        print(agent.stdout.strip())
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
