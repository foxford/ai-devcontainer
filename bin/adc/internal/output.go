package shared

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// WriteTable uses spaces so columns align in terminals and copied output.
func WriteTable(out io.Writer, headers []string, rows [][]string) error {
	clean := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && utf8.RuneCountInString(clean.Replace(cell)) > widths[i] {
				widths[i] = utf8.RuneCountInString(clean.Replace(cell))
			}
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	write := func(row []string) error {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = clean.Replace(cell)
		}
		_, err := fmt.Fprintln(w, strings.Join(cells, "\t"))
		return err
	}
	if err := write(headers); err != nil {
		return err
	}
	rule := make([]string, len(headers))
	for i, width := range widths {
		rule[i] = strings.Repeat("─", width)
	}
	if err := write(rule); err != nil {
		return err
	}
	for _, row := range rows {
		if err := write(row); err != nil {
			return err
		}
	}
	return w.Flush()
}
