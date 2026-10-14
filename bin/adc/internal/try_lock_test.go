package shared

import (
	"errors"
	"testing"
	"time"
)

func TestTryLockedSkipsBusyWriterAndReleasesAfterFailure(t *testing.T) {
	a := &App{State: t.TempDir()}
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.Locked(func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	t.Cleanup(func() { close(release); <-done })
	type result struct {
		acquired bool
		err      error
	}
	tried := make(chan result, 1)
	go func() {
		acquired, err := a.TryLocked(func() error { return errors.New("busy callback must not run") })
		tried <- result{acquired, err}
	}()
	select {
	case r := <-tried:
		if r.acquired || r.err != nil {
			t.Fatalf("busy lock: acquired=%v err=%v", r.acquired, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background refresh waited for the active writer")
	}

	other := &App{State: t.TempDir()}
	failure := errors.New("index failed")
	acquired, err := other.TryLocked(func() error { return failure })
	if !acquired || !errors.Is(err, failure) {
		t.Fatalf("callback failure lost: acquired=%v err=%v", acquired, err)
	}
	acquired, err = other.TryLocked(func() error { return nil })
	if !acquired || err != nil {
		t.Fatalf("failed callback retained the lock: acquired=%v err=%v", acquired, err)
	}
}
