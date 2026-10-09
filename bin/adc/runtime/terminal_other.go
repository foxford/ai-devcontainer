//go:build !linux

package runtime

import (
	"errors"
	"os"
)

// Interactive runtime prompts run inside Linux; host CLI forwards them.
func isTerminal(file *os.File) bool { return false }
func secretLine(file *os.File) (string, error) {
	return "", errors.New("ввод секретов доступен в Linux runtime")
}
