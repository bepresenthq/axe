package preview

import (
	"howett.net/plist"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunbpSimulatorEntitlements(t *testing.T) {
	original := map[string]any{"com.apple.security.application-groups": []any{"group.dev.fixture"}, "custom-capability": "resolved-value"}
	data, _ := plist.Marshal(original, plist.XMLFormat)
	encoded, err := runbpSimulatorEntitlements(data, "Fixture.entitlements")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if _, err := plist.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["get-task-allow"] != true || got["custom-capability"] != "resolved-value" {
		t.Fatalf("lost entitlements: %#v", got)
	}
	if got["com.apple.security.application-groups"].([]any)[0] != "group.dev.fixture" {
		t.Fatal(got)
	}
	if _, err := runbpSimulatorEntitlements(nil, "Fixture.entitlements"); err == nil || !strings.Contains(err.Error(), "no embedded entitlements") {
		t.Fatalf("missing capability accepted: %v", err)
	}
	if _, err := runbpSimulatorEntitlements([]byte("invalid"), ""); err == nil {
		t.Fatal("invalid plist accepted")
	}
	if _, err := runbpSimulatorEntitlements(nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRunbpExtensionKitBundleIDs(t *testing.T) {
	app := filepath.Join(t.TempDir(), "App.app")
	for _, directory := range []string{"PlugIns", "Extensions"} {
		file := filepath.Join(app, directory, "Report.appex", "Info.plist")
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.example.app.Report</string></dict></plist>`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rewriteEmbeddedAppExtensionBundleIDs(app, "com.example.app", "axe.com.example.app")
	for _, directory := range []string{"PlugIns", "Extensions"} {
		data, err := os.ReadFile(filepath.Join(app, directory, "Report.appex", "Info.plist"))
		if err != nil || !strings.Contains(string(data), "axe.com.example.app.Report") {
			t.Fatalf("%s: %s %v", directory, data, err)
		}
	}
}
