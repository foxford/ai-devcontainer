package shared

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func Run(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
func Output(dir, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("%s: %w: %s", name, e, b)
	}
	return strings.TrimSpace(string(b)), nil
}

func Contains(ss []string, s string) bool {
	for _, v := range ss {
		if s == v {
			return true
		}
	}
	return false
}
func RegexpID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s = strings.Trim(b.String(), "-")
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		s = "repo-" + s
	}
	if len(s) > 38 {
		s = s[:38]
	}
	return s
}
func Today() string { return time.Now().UTC().Format("2006-01-02") }
