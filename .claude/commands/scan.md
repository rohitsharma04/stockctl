Scan stocks for breakout/momentum signals using stockctl.

Parse $ARGUMENTS as: [market] [strategy] [--tickers path]
- If market is provided (e.g., "india", "us"), use `--market <market>`
- If strategy is provided (e.g., "breakout-caution"), use that; otherwise default to "all"
- If tickers path is provided, use `--tickers <path>`

Steps:
1. Build the binary if needed: `go build -o stockctl .`
2. Run the scan command:
   ```bash
   ./stockctl scan <strategy> --market <market> --tickers <path> --output json --workers 8
   ```
3. Parse the JSON output and present a summary table of results
4. Highlight stocks appearing in multiple strategies (stronger signals)
