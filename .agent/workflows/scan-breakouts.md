---
description: How to run stock breakout screeners
---

Use the `stock-analysis` skill for this task. Read the skill instructions first:

```
view_file .agent/skills/stock-analysis/SKILL.md
```

Then run the appropriate `stockctl scan` command as documented in the skill.

After scanning, inspect the top results for deeper analysis:
```bash
# For each ticker in the results, run inspect for full indicator breakdown
stockctl inspect <TICKER> --market <market> --output json
```
