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
	{MarketID: "india", Name: "NSE listed equities (EQ series)", File: "data/universes/india.csv"},
	{MarketID: "japan", Name: "Nikkei 225 (Top 100)", File: "data/universes/japan.csv"},
	{MarketID: "uk", Name: "FTSE 100", File: "data/universes/uk.csv"},
	{MarketID: "germany", Name: "DAX 40", File: "data/universes/germany.csv"},
	{MarketID: "france", Name: "CAC 40", File: "data/universes/france.csv"},
	{MarketID: "canada", Name: "S&P/TSX 60", File: "data/universes/canada.csv"},
	{MarketID: "australia", Name: "ASX 50", File: "data/universes/australia.csv"},
	{MarketID: "hong-kong", Name: "Hang Seng", File: "data/universes/hong-kong.csv"},
	{MarketID: "china", Name: "SSE 50", File: "data/universes/china.csv"},
	{MarketID: "korea", Name: "KOSPI 50", File: "data/universes/korea.csv"},
	{MarketID: "singapore", Name: "STI 30", File: "data/universes/singapore.csv"},
	{MarketID: "brazil", Name: "Ibovespa (Top 88)", File: "data/universes/brazil.csv"},
	{MarketID: "taiwan", Name: "TWSE 50", File: "data/universes/taiwan.csv"},
	{MarketID: "italy", Name: "FTSE MIB", File: "data/universes/italy.csv"},
	{MarketID: "spain", Name: "IBEX 35", File: "data/universes/spain.csv"},
	{MarketID: "sweden", Name: "OMX 30", File: "data/universes/sweden.csv"},
	{MarketID: "switzerland", Name: "SMI 20", File: "data/universes/switzerland.csv"},
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
