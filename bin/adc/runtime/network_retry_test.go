package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

type downloadTransport func(*http.Request) (*http.Response, error)

func (f downloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadRetriesDNSAndTruncatedBodies(t *testing.T) {
	attempts, waits := 0, []time.Duration{}
	client := &http.Client{Transport: downloadTransport(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, &net.DNSError{Err: "temporary resolver failure", Name: r.URL.Host}
		}
		body := io.Reader(strings.NewReader("complete installer"))
		if attempts == 2 {
			body = io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: make(http.Header)}, nil
	})}
	data, err := downloadTool(context.Background(), client, "https://example.test/install", func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return nil })
	if err != nil || string(data) != "complete installer" || attempts != 3 || fmt.Sprint(waits) != "[1s 2s]" {
		t.Fatalf("data=%q attempts=%d waits=%v error=%v", data, attempts, waits, err)
	}
}

func TestDownloadStatusRetriesAreFinite(t *testing.T) {
	for _, tc := range []struct{ status, attempts int }{{404, 1}, {403, 1}, {503, 5}, {429, 5}} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: downloadTransport(func(*http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("failure")), Header: make(http.Header)}, nil
			})}
			data, err := downloadTool(context.Background(), client, "https://example.test/install", func(context.Context, time.Duration) error { return nil })
			if err == nil || data != nil || attempts != tc.attempts {
				t.Fatalf("attempts=%d data=%q err=%v", attempts, data, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryNetwork(ctx, "fixture", func() error { attempts++; return &net.DNSError{Err: "retry"} }, func(context.Context, time.Duration) error { cancel(); return ctx.Err() })
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatal("retried after cancellation", attempts, err)
	}
}

func TestInstallerCommandRetriesNetworkFailures(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(strconv.FormatBool(permanent), func(t *testing.T) {
			testutil.Home(t)
			file := filepath.Join(t.TempDir(), "attempts")
			t.Setenv("ADC_RETRY_HELPER", file)
			t.Setenv("ADC_RETRY_PERMANENT", strconv.FormatBool(permanent))
			t.Setenv("AI_TOOLS_HOME", t.TempDir())
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			i, err := (&App{}).newToolInstaller()
			if err != nil {
				t.Fatal(err)
			}
			_, err = i.run(i.home, binary, "-test.run=^TestNetworkInstallHelperProcess$")
			if (err != nil) != permanent {
				t.Fatal("wrong install outcome", err)
			}
			data, readErr := os.ReadFile(file)
			want := "3"
			if permanent {
				want = "1"
			}
			if readErr != nil || string(data) != want {
				t.Fatalf("attempts %q %v", data, readErr)
			}
		})
	}
}

func TestNetworkInstallHelperProcess(t *testing.T) {
	file := os.Getenv("ADC_RETRY_HELPER")
	if file == "" {
		return
	}
	data, _ := os.ReadFile(file)
	n, _ := strconv.Atoi(string(data))
	if err := os.WriteFile(file, []byte(strconv.Itoa(n+1)), 0600); err != nil {
		os.Exit(2)
	}
	if os.Getenv("ADC_RETRY_PERMANENT") == "true" {
		fmt.Fprintln(os.Stderr, "npm error EACCES permission denied")
		os.Exit(1)
	}
	if n < 2 {
		fmt.Fprintln(os.Stderr, "npm error EAI_AGAIN getaddrinfo registry.npmjs.org")
		os.Exit(1)
	}
	os.Exit(0)
}
