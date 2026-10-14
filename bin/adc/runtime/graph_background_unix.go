//go:build linux || darwin

package runtime

import (
	"os"
	"os/exec"
	"syscall"
)

func detachGraphWorker(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func openGraphLogFile(path string) (*os.File, error) {
	// A log replaced after Lstat must neither redirect writes through a link nor
	// block workspace startup while opening a FIFO without a reader.
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0600)
}
