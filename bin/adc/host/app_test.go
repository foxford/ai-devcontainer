package host

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerMountScope(t *testing.T) {
	a := &App{State: filepath.Join(t.TempDir(), "state"), Platform: filepath.Join(t.TempDir(), "platform")}
	c := shared.Config{Name: "test", Port: 6770, Projects: []shared.Project{{ID: "app", Path: filepath.Join(t.TempDir(), "repo with spaces"), RuntimePath: "/projects/app", ReadOnly: true}}}
	args, e := a.DockerArgs(c)
	if e != nil {
		t.Fatal(e)
	}
	s := strings.Join(args, "\n")
	for _, want := range []string{"127.0.0.1:6770:6768", "127.0.0.1:20128:20128", "127.0.0.1:4242:4242", "dst=/projects/app,readonly", "src=adc-test-home,dst=/home/node", "ADC_RUNTIME=1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, bad := range []string{"docker.sock", "--privileged", "src=/home/"} {
		if strings.Contains(s, bad) {
			t.Fatalf("unsafe default %s", bad)
		}
	}
	if _, e = mount("/bad,path", "/target", false); e == nil {
		t.Fatal("accepted Docker delimiter")
	}
}

func TestDownloadVerifiesBeforeReplacing(t *testing.T) {
	data := []byte("release")
	sum := sha256.Sum256(data)
	client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	target := filepath.Join(t.TempDir(), "asset")
	a := Artifact{URL: "https://releases.test/asset", SHA256: hex.EncodeToString(sum[:])}
	if e := download(client, a, target); e != nil {
		t.Fatal(e)
	}
	a.SHA256 = strings.Repeat("0", 64)
	if e := download(client, a, target); e == nil {
		t.Fatal("accepted bad checksum")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "release" {
		t.Fatal("bad release replaced verified artifact")
	}
}

func TestZipTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.zip")
	f, e := os.Create(archive)
	if e != nil {
		t.Fatal(e)
	}
	w := zip.NewWriter(f)
	entry, e := w.Create("../escape")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = io.WriteString(entry, "bad")
	_ = w.Close()
	_ = f.Close()
	if e = extractZip(archive, filepath.Join(dir, "output")); e == nil {
		t.Fatal("accepted zip traversal")
	}
	if _, e = os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(e) {
		t.Fatal("wrote outside extraction root")
	}
}

func TestShellInstallIsIdempotent(t *testing.T) {
	home := testutil.Home(t)
	// Profile tests must not discover a real PowerShell installation on the runner.
	t.Setenv("PATH", t.TempDir())
	if e := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# developer settings\n"), 0644); e != nil {
		t.Fatal(e)
	}
	a := &App{State: filepath.Join(home, ".adc"), Platform: filepath.Join(home, "platform with spaces")}
	// Windows profile/registry is covered by the Windows installer smoke; avoid
	// changing the test runner's registry in this filesystem unit test.
	if os.PathSeparator == '\\' {
		t.Skip("Windows profile installation requires isolated registry smoke")
	}
	for i := 0; i < 2; i++ {
		if e := a.InstallShell(&cobra.Command{Use: "adc"}); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{".zshrc", ".bashrc", ".profile"} {
		b, e := os.ReadFile(filepath.Join(home, name))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Count(string(b), "# adc:start") != 1 {
			t.Fatal("duplicate profile block")
		}
	}
	b, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(b), "# developer settings") {
		t.Fatal("user profile overwritten")
	}
	for _, s := range []string{"bash", "zsh", "fish", "powershell"} {
		if info, e := os.Stat(filepath.Join(a.State, "completions", "adc."+s)); e != nil || info.Size() == 0 {
			t.Fatalf("no completion for %s", s)
		}
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
