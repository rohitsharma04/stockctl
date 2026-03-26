package marketdata

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// UniverseSource defines how to fetch index constituents for a market.
type UniverseSource struct {
	MarketID string
	Index    string
	URL      string
	Parser   func(body io.Reader) ([]string, error)
}

// DefaultUniverseSources returns the known index sources.
func DefaultUniverseSources() map[string]UniverseSource {
	return map[string]UniverseSource{
		"us": {
			MarketID: "us", Index: "S&P 500",
			URL:    "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies",
			Parser: parseWikipediaTable,
		},
		"india": {
			MarketID: "india", Index: "Nifty 500",
			URL:    "https://archives.nseindia.com/content/indices/ind_nifty500list.csv",
			Parser: parseNSECSV,
		},
		"japan": {
			MarketID: "japan", Index: "Nikkei 225",
			URL:    "https://en.wikipedia.org/wiki/Nikkei_225",
			Parser: parseWikipediaTable,
		},
		"uk": {
			MarketID: "uk", Index: "FTSE 100",
			URL:    "https://en.wikipedia.org/wiki/FTSE_100_Index",
			Parser: parseWikipediaTable,
		},
		"germany": {
			MarketID: "germany", Index: "DAX 40",
			URL:    "https://en.wikipedia.org/wiki/DAX",
			Parser: parseWikipediaTable,
		},
	}
}

// UniverseCacheDir returns the directory for cached ticker universe files.
func UniverseCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "stockctl", "universes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// UniverseCachePath returns the file path for a cached market universe.
func UniverseCachePath(marketID string) (string, error) {
	dir, err := UniverseCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, marketID+".csv"), nil
}

// IsCacheValid checks if the cached file exists and is younger than maxAge.
func IsCacheValid(path string, maxAge time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < maxAge
}

// FetchUniverse downloads and caches the ticker universe for a market.
func FetchUniverse(marketID string, force bool) ([]string, error) {
	sources := DefaultUniverseSources()
	src, ok := sources[marketID]
	if !ok {
		return nil, fmt.Errorf("no auto-download source for market %q (use --tickers flag)", marketID)
	}

	cachePath, err := UniverseCachePath(marketID)
	if err != nil {
		return nil, err
	}

	// Check cache (7-day TTL)
	if !force && IsCacheValid(cachePath, 7*24*time.Hour) {
		return readTickerCSV(cachePath)
	}

	// Fetch
	fmt.Printf("📥 Fetching %s constituents from %s...\n", src.Index, src.URL)
	resp, err := http.Get(src.URL)
	if err != nil {
		// Fall back to cache if available
		if tickers, err2 := readTickerCSV(cachePath); err2 == nil && len(tickers) > 0 {
			fmt.Printf("⚠ Fetch failed, using cached data (%d tickers)\n", len(tickers))
			return tickers, nil
		}
		return nil, fmt.Errorf("fetching %s: %w", src.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, src.URL)
	}

	tickers, err := src.Parser(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", src.URL, err)
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("no tickers parsed from %s", src.URL)
	}

	// Cache to disk
	if err := writeTickerCSV(cachePath, tickers); err != nil {
		fmt.Printf("⚠ Failed to cache: %v\n", err)
	} else {
		fmt.Printf("✅ Cached %d tickers to %s\n", len(tickers), cachePath)
	}

	return tickers, nil
}

// LoadUniverse loads the ticker universe for a market from cache.
func LoadUniverse(marketID string) ([]string, error) {
	cachePath, err := UniverseCachePath(marketID)
	if err != nil {
		return nil, err
	}
	return readTickerCSV(cachePath)
}

// parseWikipediaTable extracts ticker symbols from the first table on a Wikipedia page.
// Looks for a column with header containing "Symbol" or "Ticker".
func parseWikipediaTable(body io.Reader) ([]string, error) {
	doc, err := html.Parse(body)
	if err != nil {
		return nil, err
	}

	var tickers []string
	var inTable, inThead, inTbody bool
	var symCol int = -1
	var colIdx int

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "table":
				// Only process the first "wikitable" or "sortable" table
				if !inTable {
					for _, a := range n.Attr {
						if a.Key == "class" && (strings.Contains(a.Val, "wikitable") || strings.Contains(a.Val, "sortable")) {
							inTable = true
							break
						}
					}
				}
			case "thead":
				if inTable {
					inThead = true
				}
			case "tbody":
				if inTable {
					inTbody = true
				}
			case "tr":
				colIdx = 0
			case "th":
				if inTable && (inThead || !inTbody) {
					text := extractText(n)
					lower := strings.ToLower(text)
					if strings.Contains(lower, "symbol") || strings.Contains(lower, "ticker") || strings.Contains(lower, "code") {
						symCol = colIdx
					}
					colIdx++
				}
			case "td":
				if inTable && inTbody && symCol >= 0 && colIdx == symCol {
					text := strings.TrimSpace(extractText(n))
					if text != "" && !strings.Contains(text, " ") {
						// Clean ticker: remove trailing notes, BRK.B → BRK-B
						text = strings.ReplaceAll(text, ".", "-")
						tickers = append(tickers, text)
					}
				}
				colIdx++
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			switch n.Data {
			case "table":
				if inTable {
					inTable = false
				}
			case "thead":
				inThead = false
			case "tbody":
				inTbody = false
			}
		}
	}
	walk(doc)

	return tickers, nil
}

// parseNSECSV parses the NSE CSV format for Nifty index constituents.
func parseNSECSV(body io.Reader) ([]string, error) {
	reader := csv.NewReader(body)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("empty CSV")
	}

	// Find Symbol column
	symIdx := -1
	for i, h := range records[0] {
		lower := strings.ToLower(strings.TrimSpace(h))
		if lower == "symbol" {
			symIdx = i
			break
		}
	}
	if symIdx == -1 {
		symIdx = 0
	}

	var tickers []string
	for _, row := range records[1:] {
		if symIdx < len(row) {
			t := strings.TrimSpace(row[symIdx])
			if t != "" {
				tickers = append(tickers, t)
			}
		}
	}
	return tickers, nil
}

// extractText recursively extracts text content from an HTML node.
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(extractText(c))
	}
	return sb.String()
}

// readTickerCSV reads a simple one-ticker-per-line CSV.
func readTickerCSV(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tickers []string
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t != "" && t != "Symbol" {
			tickers = append(tickers, t)
		}
	}
	return tickers, nil
}

// writeTickerCSV writes a simple one-ticker-per-line CSV.
func writeTickerCSV(path string, tickers []string) error {
	var sb strings.Builder
	sb.WriteString("Symbol\n")
	for _, t := range tickers {
		sb.WriteString(t + "\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}
