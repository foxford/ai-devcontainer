package runtime

import (
	"os"
	"os/exec"
)

// Production workers run only in the Linux runtime. Windows keeps this helper
// for native tests and cross-compilation of the shared CLI.
func detachGraphWorker(cmd *exec.Cmd) {}

func openGraphLogFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}
