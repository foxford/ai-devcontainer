package runtime

import (
	"os"
	"os/exec"
)

// Production workers run only in the Linux runtime. Windows keeps this helper
// for native tests and cross-compilation of the shared CLI.
func detachGraphWorker(cmd *exec.Cmd) {}

func openGraphLogFile(path string, appendMode bool) (*os.File, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	}
	return os.OpenFile(path, flags, 0600)
}
