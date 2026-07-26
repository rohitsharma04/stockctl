# NSE Paper Trading and Live-Data Discovery Plan

**Status:** Agreed planning baseline; no broker connection, live orders, or paper-trading schedule is enabled.

## Goal

Run an India/NSE **forward paper-trading** pilot while choosing the right broker/data platform for a later, tightly constrained small-capital live system.

The pilot must feel operationally real. **Live market data is a hard requirement** for paper fills, position marking, and alerts; prior-close Yahoo daily bars alone are not sufficient for this purpose.

## Non-negotiable boundaries

- NSE cash equities only for the initial pilot.
- No live orders, brokerage credentials, leverage, F&O, shorting, or intraday strategy deployment during the paper week.
- Paper trades are generated only by deterministic, versioned rules. An LLM may explain a result but must never invent or alter an order.
- Every decision must be logged with strategy version, signal timestamp, source/data timestamp, rule inputs, simulated fill, position state, and exit reason.
- Any missing live feed, stale data, exchange closure, incomplete scan, order/fill ambiguity, or breached risk gate must fail closed: no new simulated position.
- The existing stockctl backtest is exploratory only until `backtest v2` resolves its temporal and portfolio-validity issues. It must not silently tune the paper strategy.

## Forward paper-trading mechanics

1. **Pre-open signal creation:** At 08:30 IST, stockctl records a dated NSE signal snapshot using only completed prior-session data and a strict scan-quality gate.
2. **Live paper entry:** At/after the permitted session time, the paper executor uses a live broker/vendor quote stream to model an entry price and records the quote timestamp, bid/ask or LTP policy, and slippage assumption. It never treats an old EOD close as a live fill.
3. **Position management:** Live quote updates drive marking and alerting. Stop, target, holding-period, and risk rules are evaluated deterministically. The chosen fill convention must be explicit.
4. **Close/reconciliation:** Record simulated exits, fees/slippage, cash, exposure, realised/unrealised P&L, and a daily ledger. Reconcile paper positions with the rule engine every session.
5. **Daily Telegram report:** Concise operational update showing new intents/fills, open positions, exits, risk usage, data-health status, and no-trade reasons. It is a system report, not investment advice.

## Provisional paper mandate to validate during research

These are deliberately provisional controls, not a trading recommendation:

- Virtual capital: ₹100,000.
- Maximum concurrent positions: 3.
- Maximum nominal allocation per position: ₹25,000.
- Cash equities / CNC-equivalent only; no leverage or derivatives.
- Initial entries should be based on an explicit breakout rule from a versioned stockctl snapshot, with no entry if the market or data-quality gate is not met.
- The final entry/exit, stop/target, holding-period, market-regime, and cost assumptions are research decisions. Do not enable a schedule until they are specified and tested.

A zero-trade week is valid and preferable to weakening the rules merely to manufacture activity.

## Platform and data research questions for the next session

### 1. Live-data suitability

Compare Zerodha Kite Connect, Upstox, FYERS, and any appropriate licensed NSE/BSE data source for:

- Live quote latency, WebSocket reliability, reconnect semantics, rate limits, market-depth/bid-ask availability, and data terms.
- Historical/intraday data retention, corporate actions, instrument mapping, symbol changes, and adjustment policy.
- API credential/login lifecycle, including daily session expiry and secure local OAuth/login handoff.
- Local data retention, export rights, and whether data can be used for paper trading and research.

### 2. Broker/execution suitability

Determine current India broker/API requirements and restrictions for personal automated/paper trading. Confirm order types, API availability/costs, session/token management, RMS constraints, postbacks, order reconciliation, and any current regulatory/broker obligations.

### 3. Architecture decision

Select one design after evidence gathering:

- **Preferred starting point:** stockctl signal/data layer + a separate local paper-execution service + broker live-data adapter.
- **Alternative:** a research platform only if it genuinely provides portable, auditable signals and live paper execution without surrendering data/control.

Credentials must live in macOS Keychain/secure local storage. No passwords, TOTP secrets, API secrets, access tokens, or broker credentials may be placed in source control, chat, or plain-text configuration.

### 4. Strategy and evaluation design

Specify one small, deterministic pilot strategy. Define signal timing, exact entry rule, position sizing, stop/target or time exit, live-fill/slippage policy, daily loss/exposure limits, and an operator kill switch.

Separately define `backtest v2`: historical signal replay, next-session execution, portfolio accounting, costs, conservative same-bar handling, walk-forward validation, and point-in-time universe/action data. A backtest engine does not repair weak source data.

## Go/no-go gates before live capital

1. A local, read-only broker connection has been authenticated and tested without placing an order.
2. Live data is observed through a full market session and reconnect/failure behavior is logged.
3. The paper ledger runs for an agreed evaluation period with complete audit records.
4. Paper fills and position state reconcile deterministically with the live-data record.
5. The strategy’s forward paper results, drawdowns, turnover, and operational failure modes are reviewed; no claim of edge is made from a single week.
6. Explicit live-risk mandate is approved: small capital ceiling, per-order cap, exposure cap, daily loss cap, allowed symbols/order types, operator kill switch, and approval/autonomy level.
7. Only then: initially broker orders require per-order human approval; any pre-authorised automation is a later, separately reviewed decision.

## Current known starting point

- Rohit has a Zerodha account but API/developer access is not yet confirmed.
- Kite Connect requires a trading account, TOTP 2FA, developer-portal app, API key/secret, and redirect URL. Its access-token lifecycle must be incorporated into the architecture.
- Existing stockctl provides NSE screening, local historical cache, JSON output, and exchange-calendar-aware report automation. It currently relies on Yahoo for broad research data; that feed is not sufficient as the live execution/paper-fill source.

## Next-session kickoff

Start with a deep, source-verified India market-data + broker/API + paper-execution research pack. Make a platform decision only after the live-data and operating constraints are verified. No production schedule or broker integration should be activated before that research, implementation review, tests, and an observed dry run.
