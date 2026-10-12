package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Go proxy forwards to the real Docker CLI. It can inject a missing-image
// run or a lost response after a successful rename, without touching other
// workspaces. No Docker commands or lifecycle behavior are simulated here.
func dockerUpdateFailureProxy(t *testing.T, tmp string) (string, string) {
	t.Helper()
	real, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "docker-update-proxy")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "main.go")
	const code = `package main
import (
 "fmt"
 "os"
 "os/exec"
 "strings"
)
func main() {
 args := os.Args[1:]
 marker := os.Getenv("ADC_SMOKE_DOCKER_FAILURE")
 mode, _ := os.ReadFile(marker)
 inject := len(args) > 0 && (string(mode) == "run" && args[0] == "run" || string(mode) == "rename" && args[0] == "rename")
 if inject {
  if err := os.Remove(marker); err != nil { panic(err) }
  if string(mode) == "run" {
   args = append(args[:len(args)-1], "--pull=never", "sha256:"+strings.Repeat("0", 64))
  }
 }
 command := exec.Command(os.Getenv("ADC_SMOKE_REAL_DOCKER"), args...)
 command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
 if err := command.Run(); err != nil {
  if status, ok := err.(*exec.ExitError); ok { os.Exit(status.ExitCode()) }
  fmt.Fprintln(os.Stderr, err)
  os.Exit(1)
 }
 if inject && string(mode) == "rename" {
  fmt.Fprintln(os.Stderr, "ADC_SMOKE_LOST_RENAME_RESPONSE")
  os.Exit(99)
 }
}
`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "docker"), source)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Docker proxy: %v %s", err, data)
	}
	return dir, real
}
