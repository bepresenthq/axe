package preview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k-kohey/axe/internal/preview/build"
)

func TestRunbpHostIdentityPrecedesLaunch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNBP_CONTROL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte(`{"id":"start","revision":"rev"}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings := &build.Settings{BundleID: "axe.com.example.app.debug", OriginalBundleID: "com.example.app.debug", TargetName: "App"}
	// A launch failure must still leave the attempted host's actual identity,
	// allowing the controller to distinguish a dead app from a live watcher.
	launchFailure := errors.New("fixture launch failed")
	runner := &fakeAppRunner{launchErr: launchFailure, onLaunch: func() {
		if _, err := os.Stat(filepath.Join(dir, "host.json")); err != nil {
			t.Fatalf("identity not present before launch: %v", err)
		}
	}}
	if err := launchWithHotReload(context.Background(), settings, "loader", "thunk", "socket", "device", "", runner); !errors.Is(err, launchFailure) {
		t.Fatalf("launch error lost: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "host.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got runbpHostIdentity
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != "runbp.preview.host.v1" || got.BundleID != settings.BundleID || got.DeviceID != "device" || got.BackendPID != os.Getpid() || got.RequestID != "start" || got.Revision != "rev" {
		t.Fatalf("bad identity: %#v", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.LaunchRequestedAt); err != nil {
		t.Fatal(err)
	}
	// Replacement rather than append leaves one complete JSON object.
	settings.BundleID = "axe.com.example.changed"
	if err := runbpWriteHostIdentity(dir, settings, "device"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "host.json"))
	if err := json.Unmarshal(data, &got); err != nil || got.BundleID != settings.BundleID {
		t.Fatalf("stale metadata: %#v %v", got, err)
	}
}

func TestRunbpHostIdentityRejectsUnrewrittenBundle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Info.plist")
	if err := os.WriteFile(file, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.example.app</string></dict></plist>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runbpValidateHostBundleID(file, "axe.com.example.app"); err == nil {
		t.Fatal("unrewritten identity accepted")
	}
	if err := runbpValidateHostBundleID(file, "com.example.app"); err != nil {
		t.Fatal(err)
	}
}

func TestRunbpResolvedProductName(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "My Product.app")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	settings := &build.Settings{ModuleName: "MyModule", FullProductName: "My Product.app", BuiltProductsDir: root}
	got, err := resolveAppBundle(settings, previewDirs{})
	if err != nil || got != app {
		t.Fatalf("resolved %q: %v", got, err)
	}
	if !build.HasPreviousBuild(settings, build.ProjectDirs{}) {
		t.Fatal("resolved app not reusable")
	}
}
