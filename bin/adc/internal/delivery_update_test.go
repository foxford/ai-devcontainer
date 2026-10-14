package shared

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateDeliveryInstallsFullSourceWithoutChangingPlatform(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, upstream := updateFixture(t)
	base := updatesGit(t, a.Platform, "rev-parse", "HEAD")
	changeUpdateFixture(t, upstream)
	updatesWrite(t, upstream, "delivery/instructions/base.md", "New shared contract\n")
	updatesWrite(t, upstream, "runtime/DO-NOT-INSTALL", "runtime must stay unchanged")
	updatesWrite(t, upstream, "delivery/skills/humanizer/scripts/run.py", strings.Repeat("# retained source\n", 10000))
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "update-index", "--chmod=+x", "delivery/skills/humanizer/scripts/run.py")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "full skill and contract")
	applies := 0
	apply := func() error {
		applies++
		text, err := a.InstructionText(nil)
		if err != nil || !strings.Contains(string(text), "New shared contract") {
			t.Fatalf("callback saw old instructions: %q %v", text, err)
		}
		return nil
	}
	changes, err := a.UpdateDelivery(ctx, apply)
	if err != nil || len(changes) != 3 || applies != 1 {
		t.Fatalf("update: %+v, calls=%d, err=%v", changes, applies, err)
	}
	if a.DeliveryRoot == "" || updatesGit(t, a.Platform, "rev-parse", "HEAD") != base || updatesGit(t, a.Platform, "status", "--porcelain") != "" {
		t.Fatal("delivery update changed the platform clone or did not select downloaded source")
	}
	if updatesGit(t, a.Platform, "rev-parse", "origin/main") != base {
		t.Fatal("changed platform refs")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(a.DeliveryRoot), "runtime")); !os.IsNotExist(err) {
		t.Fatal("download included runtime")
	}
	script, err := os.Stat(a.DeliveryPath("skills/humanizer/scripts/run.py"))
	if err != nil || script.Size() < 64<<10 || (runtime.GOOS != "windows" && script.Mode().Perm()&0111 == 0) {
		t.Fatalf("full executable skill source was lost: %v %v", script, err)
	}
	status, err := a.CheckUpdates(ctx, false)
	if err != nil || status.Available || !status.Cached {
		t.Fatalf("installed delivery still offered: %+v %v", status, err)
	}
	root := a.DeliveryRoot
	changes, err = a.UpdateDelivery(ctx, apply)
	if err != nil || len(changes) != 0 || applies != 2 || a.DeliveryRoot != root {
		t.Fatalf("repeat should reapply same release: %+v calls=%d %v", changes, applies, err)
	}
	// A later full update takes its own checked-out delivery as the source.
	updatesGit(t, a.Platform, "pull", "--quiet", "--ff-only")
	if err := a.RefreshDelivery(); err != nil || a.DeliveryRoot != "" {
		t.Fatalf("full update did not supersede downloaded source: %q %v", a.DeliveryRoot, err)
	}
}

func TestUpdateDeliveryValidationFailureKeepsSelectedSource(t *testing.T) {
	a, upstream := updateFixture(t)
	changeUpdateFixture(t, upstream)
	if _, err := a.UpdateDelivery(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	root := a.DeliveryRoot
	marker := filepath.Join(a.State, "delivery", "current.json")
	before, _ := os.ReadFile(marker)
	updatesWrite(t, upstream, "delivery/catalog.json", `{"version":2}`)
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "unsupported catalog")
	called := false
	_, err := a.UpdateDelivery(context.Background(), func() error { called = true; return nil })
	after, _ := os.ReadFile(marker)
	if err == nil || called || a.DeliveryRoot != root || !bytes.Equal(before, after) {
		t.Fatalf("invalid release replaced working source: %v callback=%v", err, called)
	}
	if !strings.Contains(err.Error(), "adc update") {
		t.Fatal("missing recovery guidance", err)
	}
}

func TestUpdateDeliveryFetchFailureAndCancellationDoNotApply(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing remote", true: "canceled"}[canceled], func(t *testing.T) {
			a, _ := updateFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if canceled {
				cancel()
			} else {
				updatesGit(t, a.Platform, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing"))
			}
			called := false
			if _, err := a.UpdateDelivery(ctx, func() error { called = true; return nil }); err == nil || called {
				t.Fatalf("fetch failure applied delivery: %v %v", err, called)
			}
			if _, err := os.Stat(filepath.Join(a.State, "delivery", "current.json")); !os.IsNotExist(err) {
				t.Fatal("failed download activated a source", err)
			}
		})
	}
}

func TestUpdateDeliveryApplicationFailureCanBeRetried(t *testing.T) {
	a, upstream := updateFixture(t)
	changeUpdateFixture(t, upstream)
	failure := errors.New("project is not writable")
	_, err := a.UpdateDelivery(context.Background(), func() error { return failure })
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "повторите adc update --delivery") || a.DeliveryRoot == "" {
		t.Fatalf("application failure lost source or recovery: %v", err)
	}
	called := false
	_, err = a.UpdateDelivery(context.Background(), func() error { called = true; return nil })
	if err != nil || !called {
		t.Fatal("same release was not reapplied", err)
	}
}

func TestUpdateDeliveryRejectsDowngradesAndRelativeState(t *testing.T) {
	a, upstream := updateFixture(t)
	changeUpdateFixture(t, upstream)
	if _, err := a.UpdateDelivery(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	root := a.DeliveryRoot
	updatesWrite(t, upstream, "delivery/skills/humanizer/meta.json", `{"kind":"skill","name":"humanizer","version":"1.5.0"}`)
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "older unit")
	called := false
	_, err := a.UpdateDelivery(context.Background(), func() error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "старее установленной") || called || root != a.DeliveryRoot {
		t.Fatalf("downgraded an installed unit: %v", err)
	}
	a.State = "relative-state"
	_, err = a.UpdateDelivery(context.Background(), func() error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "абсолютным") || called {
		t.Fatal("relative state was silently ignored", err)
	}
}

func TestDeliveryArchiveRejectsLinksTraversalAndOversizedFiles(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "../escape", Mode: 0644, Typeflag: tar.TypeReg},
		{Name: "/delivery/absolute", Mode: 0644, Typeflag: tar.TypeReg},
		{Name: "delivery/link", Linkname: "/tmp", Typeflag: tar.TypeSymlink},
		{Name: "delivery/hard", Linkname: "delivery/file", Typeflag: tar.TypeLink},
		{Name: "delivery/device", Typeflag: tar.TypeChar},
		{Name: "delivery/large", Typeflag: tar.TypeReg, Size: 17 << 20},
		{Name: "runtime/tool", Typeflag: tar.TypeReg},
	} {
		t.Run(header.Name, func(t *testing.T) {
			var data bytes.Buffer
			writer := tar.NewWriter(&data)
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			if err := extractDeliveryFiles(context.Background(), t.TempDir(), &data, false); err == nil {
				t.Fatal("accepted unsafe archive entry")
			}
		})
	}
}

func TestDeliveryArchiveDrainsProducerAfterTarEOF(t *testing.T) {
	for name, metadataOnly := range map[string]bool{"metadata": true, "full": false} {
		t.Run(name, func(t *testing.T) {
			failure := errors.New("archive stream failed after tar EOF")
			for name, tailErr := range map[string]error{"padding": nil, "stream error": failure} {
				t.Run(name, func(t *testing.T) {
					var archive bytes.Buffer
					tarWriter := tar.NewWriter(&archive)
					if err := tarWriter.WriteHeader(&tar.Header{Name: "delivery/skills/demo/meta.json", Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
						t.Fatal(err)
					}
					if err := tarWriter.Close(); err != nil {
						t.Fatal(err)
					}
					input, output := io.Pipe()
					defer input.Close()
					defer output.Close()
					// An unbuffered pipe reproduces small OS pipes on every platform.
					// Bound failures so the producer cannot hang the whole test suite.
					timer := time.AfterFunc(5*time.Second, func() {
						_ = input.CloseWithError(context.DeadlineExceeded)
					})
					defer timer.Stop()
					done := make(chan error, 1)
					go func() {
						_, err := output.Write(archive.Bytes())
						if err == nil {
							// Git pads records beyond the two zero blocks ending a tar.
							_, err = output.Write(make([]byte, 64<<10))
						}
						_ = output.CloseWithError(tailErr)
						done <- err
					}()
					if err := extractDeliveryFiles(context.Background(), t.TempDir(), input, metadataOnly); !errors.Is(err, tailErr) {
						t.Fatalf("expected stream error %v, got %v", tailErr, err)
					}
					if err := <-done; err != nil {
						t.Fatalf("archive producer could not finish writing: %v", err)
					}
				})
			}
		})
	}
}
