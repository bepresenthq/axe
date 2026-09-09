package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
