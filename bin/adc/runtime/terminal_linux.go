//go:build linux

package runtime

import (
	"bufio"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func terminalState(file *os.File) (syscall.Termios, error) {
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	if err != 0 {
		return state, err
	}
	return state, nil
}
func isTerminal(file *os.File) bool { _, err := terminalState(file); return err == nil }
func secretLine(file *os.File) (string, error) {
	state, err := terminalState(file)
	if err != nil {
		return "", err
	}
	masked := state
	masked.Lflag &^= syscall.ECHO
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&masked)))
	if errno != 0 {
		return "", errno
	}
	defer syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&state)))
	line, err := bufio.NewReader(file).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}
