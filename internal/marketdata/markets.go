package marketdata

import "sort"

// Market defines a stock market with its Yahoo Finance configuration.
type Market struct {
	ID             string  // short key: us, india, japan, etc.
	Name           string  // display name
	Suffix         string  // Yahoo Finance ticker suffix (e.g., ".NS" for NSE India)
	Benchmark      string  // benchmark index symbol (already includes suffix)
	Currency       string  // currency code
	CurrencySymbol string  // currency display symbol ($, ₹, ¥, etc.)
	MinPrice       float64 // minimum price filter for screeners
	Timezone       string  // IANA timezone for display and scheduling metadata
	SessionOpen    string  // local market open time, HH:MM
	SessionClose   string  // local market close time, HH:MM
}

// Markets is the registry of supported stock markets.
var Markets = map[string]Market{
	"us": {
		ID: "us", Name: "United States", Suffix: "",
		Benchmark: "^GSPC", Currency: "USD", CurrencySymbol: "$", MinPrice: 5.0,
		Timezone: "America/New_York", SessionOpen: "09:30", SessionClose: "16:00",
	},
	"india": {
		ID: "india", Name: "India (NSE)", Suffix: ".NS",
		Benchmark: "^NSEI", Currency: "INR", CurrencySymbol: "₹", MinPrice: 10.0,
		Timezone: "Asia/Kolkata", SessionOpen: "09:15", SessionClose: "15:30",
	},
	"india-bse": {
		ID: "india-bse", Name: "India (BSE)", Suffix: ".BO",
		Benchmark: "^BSESN", Currency: "INR", CurrencySymbol: "₹", MinPrice: 10.0,
		Timezone: "Asia/Kolkata", SessionOpen: "09:15", SessionClose: "15:30",
	},
	"japan": {
		ID: "japan", Name: "Japan", Suffix: ".T",
		Benchmark: "^N225", Currency: "JPY", CurrencySymbol: "¥", MinPrice: 100.0,
		Timezone: "Asia/Tokyo", SessionOpen: "09:00", SessionClose: "15:30",
	},
	"uk": {
		ID: "uk", Name: "United Kingdom", Suffix: ".L",
		Benchmark: "^FTSE", Currency: "GBP", CurrencySymbol: "£", MinPrice: 0.10,
		Timezone: "Europe/London", SessionOpen: "08:00", SessionClose: "16:30",
	},
	"germany": {
		ID: "germany", Name: "Germany", Suffix: ".DE",
		Benchmark: "^GDAXI", Currency: "EUR", CurrencySymbol: "€", MinPrice: 1.0,
		Timezone: "Europe/Berlin", SessionOpen: "09:00", SessionClose: "17:30",
	},
	"france": {
		ID: "france", Name: "France", Suffix: ".PA",
		Benchmark: "^FCHI", Currency: "EUR", CurrencySymbol: "€", MinPrice: 1.0,
		Timezone: "Europe/Paris", SessionOpen: "09:00", SessionClose: "17:30",
	},
	"canada": {
		ID: "canada", Name: "Canada", Suffix: ".TO",
		Benchmark: "^GSPTSE", Currency: "CAD", CurrencySymbol: "CA$", MinPrice: 1.0,
		Timezone: "America/Toronto", SessionOpen: "09:30", SessionClose: "16:00",
	},
	"australia": {
		ID: "australia", Name: "Australia", Suffix: ".AX",
		Benchmark: "^AXJO", Currency: "AUD", CurrencySymbol: "A$", MinPrice: 0.10,
		Timezone: "Australia/Sydney", SessionOpen: "10:00", SessionClose: "16:00",
	},
	"hong-kong": {
		ID: "hong-kong", Name: "Hong Kong", Suffix: ".HK",
		Benchmark: "^HSI", Currency: "HKD", CurrencySymbol: "HK$", MinPrice: 1.0,
		Timezone: "Asia/Hong_Kong", SessionOpen: "09:30", SessionClose: "16:00",
	},
	"china": {
		ID: "china", Name: "China (Shanghai)", Suffix: ".SS",
		Benchmark: "000001.SS", Currency: "CNY", CurrencySymbol: "¥", MinPrice: 1.0,
		Timezone: "Asia/Shanghai", SessionOpen: "09:30", SessionClose: "15:00",
	},
	"korea": {
		ID: "korea", Name: "South Korea", Suffix: ".KS",
		Benchmark: "^KS11", Currency: "KRW", CurrencySymbol: "₩", MinPrice: 1000.0,
		Timezone: "Asia/Seoul", SessionOpen: "09:00", SessionClose: "15:30",
	},
	"singapore": {
		ID: "singapore", Name: "Singapore", Suffix: ".SI",
		Benchmark: "^STI", Currency: "SGD", CurrencySymbol: "S$", MinPrice: 0.10,
		Timezone: "Asia/Singapore", SessionOpen: "09:00", SessionClose: "17:00",
	},
	"brazil": {
		ID: "brazil", Name: "Brazil", Suffix: ".SA",
		Benchmark: "^BVSP", Currency: "BRL", CurrencySymbol: "R$", MinPrice: 1.0,
		Timezone: "America/Sao_Paulo", SessionOpen: "10:00", SessionClose: "17:00",
	},
	"taiwan": {
		ID: "taiwan", Name: "Taiwan", Suffix: ".TW",
		Benchmark: "^TWII", Currency: "TWD", CurrencySymbol: "NT$", MinPrice: 10.0,
		Timezone: "Asia/Taipei", SessionOpen: "09:00", SessionClose: "13:30",
	},
	"italy": {
		ID: "italy", Name: "Italy", Suffix: ".MI",
		Benchmark: "FTSEMIB.MI", Currency: "EUR", CurrencySymbol: "€", MinPrice: 1.0,
		Timezone: "Europe/Rome", SessionOpen: "09:00", SessionClose: "17:30",
	},
	"spain": {
		ID: "spain", Name: "Spain", Suffix: ".MC",
		Benchmark: "^IBEX", Currency: "EUR", CurrencySymbol: "€", MinPrice: 1.0,
		Timezone: "Europe/Madrid", SessionOpen: "09:00", SessionClose: "17:30",
	},
	"sweden": {
		ID: "sweden", Name: "Sweden", Suffix: ".ST",
		Benchmark: "^OMX", Currency: "SEK", CurrencySymbol: "kr", MinPrice: 1.0,
		Timezone: "Europe/Stockholm", SessionOpen: "09:00", SessionClose: "17:30",
	},
	"switzerland": {
		ID: "switzerland", Name: "Switzerland", Suffix: ".SW",
		Benchmark: "^SSMI", Currency: "CHF", CurrencySymbol: "CHF", MinPrice: 1.0,
		Timezone: "Europe/Zurich", SessionOpen: "09:00", SessionClose: "17:30",
	},
}

// ListMarkets returns a sorted list of all market IDs.
func ListMarkets() []string {
	keys := make([]string, 0, len(Markets))
	for k := range Markets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
