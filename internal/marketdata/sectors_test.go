package marketdata

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestEmbeddedSectorMappingsCoverTheirSupportedUniverses(t *testing.T) {
	// India deliberately embeds the whole NSE EQ universe, while the official
	// Nifty 500 source only classifies its constituents. US remains exact.
	for _, market := range []string{"india", "us"} {
		t.Run(market, func(t *testing.T) {
			universe, err := GetUniverse(market)
			if err != nil {
				t.Fatalf("GetUniverse(%q): %v", market, err)
			}
			want := make(map[string]struct{}, len(universe))
			for _, symbol := range universe {
				if _, exists := want[symbol]; exists {
					t.Fatalf("embedded %s universe contains duplicate symbol %q", market, symbol)
				}
				want[symbol] = struct{}{}
			}

			data, err := sectorFS.ReadFile("data/" + market + "_sectors.csv")
			if err != nil {
				t.Fatalf("reading embedded sector mapping: %v", err)
			}
			records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
			if err != nil {
				t.Fatalf("parsing sector mapping: %v", err)
			}
			if len(records) == 0 || strings.Join(records[0], ",") != "Symbol,Sector,Industry,CapTier,Category" {
				t.Fatalf("unexpected CSV header: %v", records)
			}

			got := make(map[string]struct{}, len(records)-1)
			for line, row := range records[1:] {
				if len(row) != 5 {
					t.Fatalf("row %d has %d columns, want 5", line+2, len(row))
				}
				symbol := strings.TrimSpace(row[0])
				if symbol == "" || strings.TrimSpace(row[1]) == "" || strings.TrimSpace(row[2]) == "" {
					t.Fatalf("row %d requires non-empty Symbol, Sector, and Industry: %q", line+2, row)
				}
				if _, duplicate := got[symbol]; duplicate {
					t.Fatalf("duplicate sector mapping for %q", symbol)
				}
				got[symbol] = struct{}{}
			}
			if market == "us" && len(got) != len(want) {
				t.Fatalf("mapping has %d symbols, universe has %d", len(got), len(want))
			}
			if market == "us" {
				for symbol := range want {
					if _, ok := got[symbol]; !ok {
						t.Errorf("missing mapping for %q", symbol)
					}
				}
			}
			for symbol := range got {
				if _, ok := want[symbol]; !ok {
					t.Errorf("extra mapping for %q", symbol)
				}
			}
			if market == "india" && len(got) != 500 {
				t.Fatalf("India mapping has %d symbols, want exactly the official Nifty 500 classifications", len(got))
			}
		})
	}
}

func TestEmbeddedIndiaUniverseCoversNSEListedEquities(t *testing.T) {
	universe, err := GetUniverse("india")
	if err != nil {
		t.Fatalf("GetUniverse(India): %v", err)
	}
	unique := make(map[string]struct{}, len(universe))
	for _, symbol := range universe {
		if _, exists := unique[symbol]; exists {
			t.Fatalf("embedded India universe contains duplicate symbol %q", symbol)
		}
		unique[symbol] = struct{}{}
	}
	// NSE's official EQUITY_L.csv contains well over 2,000 current EQ-series
	// listings.  This guard prevents accidentally reverting to Nifty 500.
	if len(universe) < 2000 {
		t.Fatalf("embedded India universe has %d symbols, want at least 2,000 NSE listed equities", len(universe))
	}
	for _, symbol := range []string{"20MICRONS", "RELIANCE", "ZYDUSWELL"} {
		if _, ok := unique[symbol]; !ok {
			t.Errorf("embedded India universe is missing NSE listed equity %q", symbol)
		}
	}
}
