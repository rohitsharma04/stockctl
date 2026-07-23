package output

import "testing"

func TestParseFormat(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  Format
		ok    bool
	}{{"table", FormatTable, true}, {"json", FormatJSON, true}, {"csv", FormatCSV, true}, {"xml", "", false}} {
		got, err := ParseFormat(tc.value)
		if (err == nil) != tc.ok || got != tc.want {
			t.Fatalf("ParseFormat(%q) = %q, %v", tc.value, got, err)
		}
	}
}
