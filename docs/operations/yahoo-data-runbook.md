# Yahoo historical-data operations runbook

## Safety contract

`stockctl` owns cache mutation, rate limiting, retry/backoff, checkpointing, and resume behavior. Hermes only launches one scheduled process and reports a terminal non-zero exit. Do not run concurrent seed or broad `scan all` processes against the same cache.

The seed command is intentionally sequential today. A cold India + US seed can run for hours; use the checkpoint rather than restarting from scratch.

## Build and test

```bash
go test -race ./...
go build -o bin/stockctl .
python3 -m py_compile automation/stockctl_weekend_seed.py
```

## Start or resume a seed

Use a dedicated state file if you want an explicit run boundary. Re-running the same command resumes successful, pending, and due-retry ticker state without refetching completed tickers.

```bash
bin/stockctl --quiet seed history \
  --market india --market us \
  --period max \
  --rate 1 --workers 1 --max-attempts 3 --deadline 6h \
  --state-file ~/.stockctl/seed-history-state.json
```

Exit status is non-zero when any ticker is terminally failed or remains pending at the deadline. Stdout is one JSON summary; progress and CLI usage are suppressed in quiet mode.

The checkpoint stores the requested history period and coverage identity. Version 1
and version 2 checkpoints are treated as incompatible legacy checkpoints; the
first `--period max` run upgrades the checkpoint to version 3 and resets ticker
work so old successes do not hide missing full-history cache entries.

## Inspect checkpoint state

```bash
python3 - <<'PY'
import json
from pathlib import Path
p = Path.home() / '.stockctl' / 'seed-history-state.json'
s = json.loads(p.read_text())
counts = {}
for item in s['tickers'].values():
    counts[item['status']] = counts.get(item['status'], 0) + 1
print({'updated_at': s.get('updated_at'), 'markets': s.get('markets'), 'period': s.get('period'), 'counts': counts})
PY
```

Statuses:

- `success`: completed and skipped on resume.
- `pending`: not started or interrupted; eligible next invocation.
- `retry`: has a persisted `next_retry`; it is skipped until that time.
- `failed`: non-transient failure or exhausted retry budget; requires investigation before a reset.

## Recover from failure

1. **429 / network outage:** let the next scheduled run resume. Do not delete the checkpoint or start concurrent manual runs.
2. **Deadline:** increase `--deadline` only after confirming no other provider-intensive job is running. Interrupted work remains `pending`.
3. **Terminal ticker error:** inspect its `last_error`. To retry only after deliberate review, edit or remove that ticker's state entry while preserving the rest of the checkpoint. Back up first:

   ```bash
   cp ~/.stockctl/seed-history-state.json ~/.stockctl/seed-history-state.backup.json
   ```

4. **Corrupt checkpoint:** preserve the corrupt file for diagnosis, then move it aside. A new seed rebuilds state but may re-check all symbols:

   ```bash
   mv ~/.stockctl/seed-history-state.json ~/.stockctl/seed-history-state.corrupt.json
   ```

## Hermes launcher and future cron

The versioned launcher is intentionally policy-free:

```bash
python3 automation/stockctl_weekend_seed.py \
  --market india --market us --period max --rate 1 --workers 1 --max-attempts 3 --deadline 6h --quiet
```

It resolves `STOCKCTL_BIN`, then `bin/stockctl`, then `go run .`, and uses `exec` so exit status and stdout are unchanged.

**Do not enable the Sunday Hermes cron until a controlled seed has completed successfully and the morning report job is confirmed not to overlap.** The intended cron policy is one no-agent Sunday 03:00 IST invocation with a six-hour process timeout. Hermes must not implement its own ticker loop, cache writes, retries, or checkpoint logic.
