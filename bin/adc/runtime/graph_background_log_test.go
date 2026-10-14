package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func assertGraphLogRejected(t *testing.T, path string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^$")
	done := make(chan error, 1)
	go func() { done <- startGraphWorker(cmd, path) }()
	select {
	case err := <-done:
		if err == nil || cmd.Process != nil {
			t.Fatal("worker accepted a nonregular log path", path, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a nonregular log blocked workspace startup", path)
	}
}

func TestGraphBackgroundRejectsForeignLogPaths(t *testing.T) {
	requireSymlinks(t)
	for _, scenario := range []string{"state-symlink", "log-symlink", "state-file", "log-directory"} {
		t.Run(scenario, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			log := graphLogPath(root)
			protected := filepath.Join(outside, "graphify.log")
			writeFixture(t, protected, "ADC_FOREIGN_CONTENT")
			switch scenario {
			case "state-symlink":
				if err := os.Symlink(outside, filepath.Dir(log)); err != nil {
					t.Fatal(err)
				}
			case "state-file":
				writeFixture(t, filepath.Dir(log), "ADC_STATE_FILE")
			case "log-symlink":
				if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(protected, log); err != nil {
					t.Fatal(err)
				}
			case "log-directory":
				if err := os.MkdirAll(log, 0700); err != nil {
					t.Fatal(err)
				}
			}
			assertGraphLogRejected(t, log)
			if readFixture(t, protected) != "ADC_FOREIGN_CONTENT" {
				t.Fatal("worker modified a file outside the checkout")
			}
		})
	}
}

func TestGraphBackgroundDoesNotTruncateForeignLog(t *testing.T) {
	a, p, control := graphBackgroundFixture(t)
	writeFixture(t, filepath.Join(control, "release"), "release")
	protected := filepath.Join(t.TempDir(), "protected")
	writeFixture(t, protected, "ADC_FOREIGN_CONTENT")
	log := graphLogPath(p.RuntimePath)
	if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, log); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshGraph(p.RuntimePath); err == nil {
		t.Fatal("active worker accepted a log symlink")
	}
	if readFixture(t, protected) != "ADC_FOREIGN_CONTENT" {
		t.Fatal("active worker truncated a file outside the checkout")
	}
	if _, err := os.Stat(filepath.Join(p.RuntimePath, "graphify-out")); !os.IsNotExist(err) {
		t.Fatal("worker started indexing after rejecting its log", err)
	}
}

func TestGraphLogAppendsUntilActiveWriterResetsIt(t *testing.T) {
	path := graphLogPath(t.TempDir())
	log, err := openGraphLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = openGraphLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if _, err := log.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, path) != "first\nsecond\n" {
		t.Fatal("opening a duplicate worker's log lost active diagnostics")
	}
	if err := log.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := log.WriteString("reset\n"); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, path) != "reset\n" {
		t.Fatal("active writer did not reset its regular log")
	}
}
