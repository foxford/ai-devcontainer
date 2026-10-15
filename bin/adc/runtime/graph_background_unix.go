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

func openGraphLogFile(path string, appendMode bool) (*os.File, error) {
	// A log replaced after Lstat must neither redirect writes through a link nor
	// block workspace startup while opening a FIFO without a reader.
	flags := os.O_CREATE | os.O_WRONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
	if appendMode {
		flags |= os.O_APPEND
	}
	return os.OpenFile(path, flags, 0600)
}
