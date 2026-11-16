package host

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEnsureImageUsesCurrentDockerContext(t *testing.T) {
	for _, context := range []string{"default", "orbstack", "desktop-linux"} {
		t.Run(context, func(t *testing.T) {
			a := &App{State: t.TempDir(), Platform: t.TempDir()}
			wantImage := "sha256:" + strings.Repeat("a", 64)
			var iidfile string
			builds := 0
			d := dockerCommands{
				output: func(args ...string) (string, error) {
					switch strings.Join(args, " ") {
					case "info --format {{.OSType}}":
						return "linux", nil
					case "context show":
						return context + "\n", nil
					default:
						t.Fatalf("unexpected Docker query: %v", args)
						return "", nil
					}
				},
				run: func(args ...string) error {
					builds++
					if !slices.Equal(args[:3], []string{"build", "--builder", context}) {
						t.Fatalf("build does not use the current context's builder: %v", args)
					}
					index := slices.Index(args, "--iidfile")
					if index < 0 || index+1 == len(args) {
						t.Fatalf("build does not capture its immutable image ID: %v", args)
					}
					iidfile = args[index+1]
					if filepath.Dir(iidfile) != a.State {
						t.Fatalf("image ID file is outside ADC state: %s", iidfile)
					}
					return os.WriteFile(iidfile, []byte(wantImage+"\n"), 0600)
				},
			}
			image, err := a.ensureImage(d)
			if err != nil || image != wantImage || builds != 1 {
				t.Fatalf("image=%q builds=%d error=%v", image, builds, err)
			}
			if _, err := os.Stat(iidfile); !os.IsNotExist(err) {
				t.Fatalf("temporary image ID file was not removed: %v", err)
			}
		})
	}
}

func TestEnsureImageStopsOnDockerFailures(t *testing.T) {
	for _, scenario := range []string{"info", "context", "empty context", "build", "missing image ID file"} {
		t.Run(scenario, func(t *testing.T) {
			a := &App{State: t.TempDir(), Platform: t.TempDir()}
			failure := errors.New("Docker fixture failure")
			builds := 0
			d := dockerCommands{
				output: func(args ...string) (string, error) {
					if args[0] == scenario {
						return "", failure
					}
					if args[0] == "context" {
						if scenario == "empty context" {
							return " \n", nil
						}
						return "orbstack", nil
					}
					return "linux", nil
				},
				run: func(args ...string) error {
					builds++
					if scenario == "missing image ID file" {
						return os.Remove(args[slices.Index(args, "--iidfile")+1])
					}
					return failure
				},
			}
			image, err := a.ensureImage(d)
			if err == nil || image != "" {
				t.Fatalf("Docker failure was ignored: image=%q error=%v", image, err)
			}
			if slices.Contains([]string{"info", "context", "build"}, scenario) && !errors.Is(err, failure) {
				t.Fatalf("original Docker error was lost: %v", err)
			}
			wantBuilds := 0
			if scenario == "build" || scenario == "missing image ID file" {
				wantBuilds = 1
			}
			if builds != wantBuilds {
				t.Fatalf("builds=%d, want %d", builds, wantBuilds)
			}
			leftovers, err := filepath.Glob(filepath.Join(a.State, ".adc-image-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary image ID files remain: %v; %v", leftovers, err)
			}
		})
	}
}
