package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunbpDeviceGate(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "ready.json")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runbpWaitForDevice(ctx, marker, "owned-device") }()
	select {
	case err := <-done:
		t.Fatalf("gate released before preparation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.WriteFile(marker, []byte(`{"udid":"owned-device"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := runbpWaitForDevice(ctx, marker, "another-device"); err == nil {
		t.Fatal("accepted wrong simulator")
	}
	if err := runbpWaitForDevice(ctx, "", "owned-device"); err != nil {
		t.Fatal(err)
	}
}

func TestRunbpDeviceGateCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runbpWaitForDevice(ctx, filepath.Join(t.TempDir(), "missing"), "owned-device"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
