Deep-analyze a single stock using stockctl inspect.

Parse $ARGUMENTS as: <ticker> [--market <market>]
- Ticker is required (e.g., "AAPL", "RELIANCE")
- If market is specified, use `--market <market>`

Steps:
1. Build the binary if needed: `go build -o stockctl .`
2. Run the inspect command:
   ```bash
   ./stockctl inspect <ticker> --market <market> --output json
   ```
3. Parse the JSON output and present:
   - Current price, volume, 52-week high/low
   - All indicator values (SMA50, SMA200, Bollinger, ATR, RS)
   - Per-screener pass/fail results with filter breakdown
4. Provide a brief analysis of the stock's technical posture
