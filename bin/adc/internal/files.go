package shared

import (
	"fmt"
	"os"
	"strings"
)

func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func ManagedBlock(path, start, end, body string) error {
	b, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	s := string(b)
	i := strings.Index(s, start)
	j := strings.Index(s, end)
	if (i >= 0) != (j >= 0) || j >= 0 && j < i {
		return fmt.Errorf("неполный управляемый блок в %s", path)
	}
	block := start + "\n" + body + "\n" + end
	if i >= 0 {
		s = s[:i] + block + s[j+len(end):]
	} else {
		s = strings.TrimRight(s, "\n") + "\n\n" + block + "\n"
	}
	return AtomicWrite(path, []byte(s), 0644)
}
