package marketdata

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed data/universes/*.csv
var universeFS embed.FS

// UniverseIndex describes an embedded ticker universe.
type UniverseIndex struct {
	MarketID string
	Name     string
	File     string // path inside embed.FS
}

// EmbeddedUniverses lists all available built-in universes.
var EmbeddedUniverses = []UniverseIndex{
	{MarketID: "us", Name: "S&P 500", File: "data/universes/us.csv"},
	{MarketID: "india", Name: "Nifty 500", File: "data/universes/india.csv"},
}

// GetUniverse returns the embedded ticker list for a market.
func GetUniverse(marketID string) ([]string, error) {
	for _, u := range EmbeddedUniverses {
		if u.MarketID == marketID {
			return readEmbeddedCSV(u.File)
		}
	}
	return nil, fmt.Errorf("no built-in universe for market %q (use --tickers flag)", marketID)
}

// ListAvailableUniverses returns the list of markets with embedded universes.
func ListAvailableUniverses() []UniverseIndex {
	return EmbeddedUniverses
}

func readEmbeddedCSV(path string) ([]string, error) {
	data, err := universeFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading embedded %s: %w", path, err)
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
