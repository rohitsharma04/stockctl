---
description: How to deep-analyze a single stock's technical indicators and screener results
---

Use the `stock-analysis` skill for this task. Read the skill instructions first:

```
view_file .agent/skills/stock-analysis/SKILL.md
```

Then run the inspect command:
```bash
# Basic usage
stockctl inspect <TICKER> --output json

# With market selection
stockctl inspect <TICKER> --market <market> --output json
```

The output includes current price, volume, 52-week range, all indicator values, and per-screener filter breakdown.
