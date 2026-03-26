package marketdata

// Market defines a stock market with its Yahoo Finance configuration.
type Market struct {
	ID        string // short key: us, india, japan, etc.
	Name      string // display name
	Suffix    string // Yahoo Finance ticker suffix (e.g., ".NS" for NSE India)
	Benchmark string // benchmark index symbol (already includes suffix)
	Currency  string // currency code
}

// Markets is the registry of supported stock markets.
var Markets = map[string]Market{
	"us": {
		ID: "us", Name: "United States", Suffix: "",
		Benchmark: "^GSPC", Currency: "USD",
	},
	"india": {
		ID: "india", Name: "India (NSE)", Suffix: ".NS",
		Benchmark: "^NSEI", Currency: "INR",
	},
	"india-bse": {
		ID: "india-bse", Name: "India (BSE)", Suffix: ".BO",
		Benchmark: "^BSESN", Currency: "INR",
	},
	"japan": {
		ID: "japan", Name: "Japan", Suffix: ".T",
		Benchmark: "^N225", Currency: "JPY",
	},
	"uk": {
		ID: "uk", Name: "United Kingdom", Suffix: ".L",
		Benchmark: "^FTSE", Currency: "GBP",
	},
	"germany": {
		ID: "germany", Name: "Germany", Suffix: ".DE",
		Benchmark: "^GDAXI", Currency: "EUR",
	},
	"france": {
		ID: "france", Name: "France", Suffix: ".PA",
		Benchmark: "^FCHI", Currency: "EUR",
	},
	"canada": {
		ID: "canada", Name: "Canada", Suffix: ".TO",
		Benchmark: "^GSPTSE", Currency: "CAD",
	},
	"australia": {
		ID: "australia", Name: "Australia", Suffix: ".AX",
		Benchmark: "^AXJO", Currency: "AUD",
	},
	"hong-kong": {
		ID: "hong-kong", Name: "Hong Kong", Suffix: ".HK",
		Benchmark: "^HSI", Currency: "HKD",
	},
	"china": {
		ID: "china", Name: "China (Shanghai)", Suffix: ".SS",
		Benchmark: "000001.SS", Currency: "CNY",
	},
	"korea": {
		ID: "korea", Name: "South Korea", Suffix: ".KS",
		Benchmark: "^KS11", Currency: "KRW",
	},
	"singapore": {
		ID: "singapore", Name: "Singapore", Suffix: ".SI",
		Benchmark: "^STI", Currency: "SGD",
	},
	"brazil": {
		ID: "brazil", Name: "Brazil", Suffix: ".SA",
		Benchmark: "^BVSP", Currency: "BRL",
	},
	"taiwan": {
		ID: "taiwan", Name: "Taiwan", Suffix: ".TW",
		Benchmark: "^TWII", Currency: "TWD",
	},
	"italy": {
		ID: "italy", Name: "Italy", Suffix: ".MI",
		Benchmark: "FTSEMIB.MI", Currency: "EUR",
	},
	"spain": {
		ID: "spain", Name: "Spain", Suffix: ".MC",
		Benchmark: "^IBEX", Currency: "EUR",
	},
	"sweden": {
		ID: "sweden", Name: "Sweden", Suffix: ".ST",
		Benchmark: "^OMX", Currency: "SEK",
	},
	"switzerland": {
		ID: "switzerland", Name: "Switzerland", Suffix: ".SW",
		Benchmark: "^SSMI", Currency: "CHF",
	},
}

// ListMarkets returns a sorted list of all market IDs.
func ListMarkets() []string {
	keys := make([]string, 0, len(Markets))
	for k := range Markets {
		keys = append(keys, k)
	}
	// Sort for consistent display
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// ApplySuffix adds the market suffix to a ticker if it doesn't already have one.
func (m Market) ApplySuffix(ticker string) string {
	if m.Suffix == "" {
		return ticker
	}
	// Don't double-apply suffix
	if len(ticker) > len(m.Suffix) && ticker[len(ticker)-len(m.Suffix):] == m.Suffix {
		return ticker
	}
	// Don't apply to index symbols (start with ^)
	if len(ticker) > 0 && ticker[0] == '^' {
		return ticker
	}
	return ticker + m.Suffix
}
