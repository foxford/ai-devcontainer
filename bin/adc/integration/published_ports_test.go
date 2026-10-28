package integration

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// The same Go test binary also serves a local HTTP fixture inside the runtime.
func TestDockerPublishedHTTPFixture(t *testing.T) {
	port := os.Getenv("ADC_PUBLISHED_HTTP_FIXTURE_PORT")
	if port == "" {
		return
	}
	if err := http.ListenAndServe("0.0.0.0:"+port, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ADC_PUBLISHED_PORT_OK\n")
	})); err != nil {
		t.Fatal(err)
	}
}

func checkPublishedPortBindings(t *testing.T, docker func(...string) string, container string, hostPort, containerPort int) {
	t.Helper()
	var bindings map[string][]struct{ HostIP, HostPort string }
	decode(t, docker("inspect", "--format", "{{json .NetworkSettings.Ports}}", container), &bindings)
	key := strconv.Itoa(containerPort) + "/tcp"
	actual := bindings[key]
	if hostPort == 0 {
		if len(actual) != 0 {
			t.Fatalf("cleared publication still binds %s: %+v", key, actual)
		}
		return
	}
	if len(actual) != 1 || actual[0].HostIP != "127.0.0.1" || actual[0].HostPort != strconv.Itoa(hostPort) {
		t.Fatalf("unexpected published binding for %s: %+v; want only 127.0.0.1:%d", key, actual, hostPort)
	}
}

func checkPublishedHTTPPort(t *testing.T, docker func(...string) string, container, fixtureBinary string, hostPort, containerPort int) {
	t.Helper()
	checkPublishedPortBindings(t, docker, container, hostPort, containerPort)
	const fixturePath = "/tmp/adc-published-http-fixture"
	docker("cp", fixtureBinary, container+":"+fixturePath)
	docker("exec", container, "chmod", "755", fixturePath)
	docker("exec", "-d", "--user", "node", "--env", "ADC_PUBLISHED_HTTP_FIXTURE_PORT="+strconv.Itoa(containerPort), container, fixturePath, "-test.run=^TestDockerPublishedHTTPFixture$")
	// A direct transport ensures proxy settings cannot hide a failed loopback publication.
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Second, Transport: transport}
	url := fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
	var lastError error
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1024))
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && string(body) == "ADC_PUBLISHED_PORT_OK\n" {
				return
			}
			err = fmt.Errorf("status %d, body %q, read error %v", response.StatusCode, body, readErr)
		}
		lastError = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("published runtime service at %s is unreachable: %v", url, lastError)
}
