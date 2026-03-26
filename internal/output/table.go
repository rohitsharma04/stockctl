package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Format represents the output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

// TableWriter writes formatted table output to a writer.
type TableWriter struct {
	w       io.Writer
	headers []string
	rows    [][]string
}

// NewTableWriter creates a new table writer.
func NewTableWriter(w io.Writer) *TableWriter {
	return &TableWriter{w: w}
}

// SetHeaders sets the table headers.
func (t *TableWriter) SetHeaders(headers ...string) {
	t.headers = headers
}

// AddRow adds a row to the table.
func (t *TableWriter) AddRow(values ...string) {
	t.rows = append(t.rows, values)
}

// Render outputs the table.
func (t *TableWriter) Render() {
	if len(t.headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, val := range row {
			if i < len(widths) && len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Print separator
	printSep := func() {
		for i, w := range widths {
			if i > 0 {
				fmt.Fprint(t.w, "─┼─")
			}
			fmt.Fprint(t.w, strings.Repeat("─", w+2))
		}
		fmt.Fprintln(t.w)
	}

	// Print headers
	printSep()
	for i, h := range t.headers {
		if i > 0 {
			fmt.Fprint(t.w, " │ ")
		}
		fmt.Fprintf(t.w, " %-*s", widths[i], h)
	}
	fmt.Fprintln(t.w)
	printSep()

	// Print rows
	for _, row := range t.rows {
		for i, val := range row {
			if i > 0 {
				fmt.Fprint(t.w, " │ ")
			}
			fmt.Fprintf(t.w, " %-*s", widths[i], val)
		}
		fmt.Fprintln(t.w)
	}
	printSep()
}

// WriteJSON writes data as formatted JSON to a writer.
func WriteJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// WriteCSV writes data as CSV to a file.
func WriteCSV(filename string, headers []string, rows [][]string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
