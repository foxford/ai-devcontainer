package shared

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"
)

// ColorEnabled keeps redirected output, dumb terminals and NO_COLOR plain.
func ColorEnabled(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminalOutput(out)
}

func IsTerminalOutput(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func noticeText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func noticeColor(value, code string, color bool) string {
	if color {
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}
	return value
}

// WriteDeliveryChanges retains explicit statuses when color is unavailable.
func WriteDeliveryChanges(out io.Writer, title string, changes []DeliveryChange, color, applied bool) error {
	code := "1;33"
	if applied {
		code = "1;32"
	}
	if _, err := fmt.Fprintln(out, noticeColor(noticeText(title), code, color)); err != nil {
		return err
	}
	for _, change := range changes {
		action, actionColor := "новая версия", "33"
		if applied {
			action, actionColor = "обновлён", "32"
		}
		switch change.Action {
		case "added":
			action, actionColor = "новый", "36"
			if applied {
				action = "добавлен"
			}
		}
		kind := change.Kind
		if kind == "mcp" {
			kind = "MCP"
		} else if kind == "document" {
			kind = "документ"
		}
		version := ""
		if change.BeforeVersion != change.AfterVersion && change.BeforeVersion != "" && change.AfterVersion != "" {
			version = " · " + change.BeforeVersion + " → " + change.AfterVersion
		} else if change.AfterVersion != "" {
			version = " · " + change.AfterVersion
		}
		if _, err := fmt.Fprintf(out, "  %s  %s %s%s\n", noticeColor(action, actionColor, color), noticeText(kind), noticeText(change.Name), noticeText(version)); err != nil {
			return err
		}
	}
	return nil
}

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
