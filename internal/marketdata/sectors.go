package marketdata

import (
	"embed"
	"encoding/csv"
	"strings"
	"sync"
)

//go:embed data/india_sectors.csv data/us_sectors.csv
var sectorFS embed.FS

// SectorInfo holds sector classification for a ticker.
type SectorInfo struct {
	Sector   string `json:"sector"`
	Industry string `json:"industry"`
	CapTier  string `json:"cap_tier"` // large, mid, small
	Category string `json:"category"` // PSU, Private, MNC (India-specific)
}

var (
	sectorCache     map[string]SectorInfo
	sectorCacheOnce sync.Once
)

func loadSectorData() {
	sectorCache = make(map[string]SectorInfo)

	files := []string{"data/india_sectors.csv", "data/us_sectors.csv"}
	for _, f := range files {
		data, err := sectorFS.ReadFile(f)
		if err != nil {
			continue
		}
		reader := csv.NewReader(strings.NewReader(string(data)))
		records, err := reader.ReadAll()
		if err != nil {
			continue
		}
		// Skip header: Symbol,Sector,Industry,CapTier,Category
		for _, row := range records[1:] {
			if len(row) < 4 {
				continue
			}
			symbol := strings.TrimSpace(row[0])
			info := SectorInfo{
				Sector:   strings.TrimSpace(row[1]),
				Industry: strings.TrimSpace(row[2]),
				CapTier:  strings.TrimSpace(row[3]),
			}
			if len(row) >= 5 {
				info.Category = strings.TrimSpace(row[4])
			}
			sectorCache[symbol] = info

			// Also store with common suffixes for lookup flexibility
			if !strings.Contains(symbol, ".") {
				// Store bare symbol so both "RELIANCE" and "RELIANCE.NS" match
				sectorCache[symbol] = info
			}
		}
	}
}

// GetSectorInfo returns sector classification for a ticker.
// It tries exact match first, then strips the suffix.
func GetSectorInfo(ticker string) (SectorInfo, bool) {
	sectorCacheOnce.Do(loadSectorData)

	// Exact match
	if info, ok := sectorCache[ticker]; ok {
		return info, true
	}

	// Strip suffix and try again
	if idx := strings.LastIndex(ticker, "."); idx > 0 {
		bare := ticker[:idx]
		if info, ok := sectorCache[bare]; ok {
			return info, true
		}
	}

	return SectorInfo{}, false
}
