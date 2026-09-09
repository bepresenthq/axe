package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestRunbpTargetNameAndModernResponse(t *testing.T) {
	dirs := ProjectDirs{Build: t.TempDir()}
	s := &Settings{ModuleName: "BePresent", TargetName: "Screentox", Configuration: "Debug", BuiltProductsDir: "/products"}
	manifest := writeDependencyManifest(t, dirs, "Screentox", nil)
	header := filepath.Join(dirs.Build, "headers with spaces")
	common := "'-I" + header + "' -iquote '/maps/Project Headers.hmap' -F '/frameworks with spaces' -Xcc '-fmodule-map-file=/maps/TikTok.modulemap'"
	if err := os.WriteFile(filepath.Join(filepath.Dir(manifest), "hash-common-args.resp"), []byte(common), 0600); err != nil {
		t.Fatal(err)
	}
	ExtractCompilerPaths(context.Background(), s, dirs)
	if !slices.Contains(s.ExtraIncludePaths, header) || !slices.Contains(s.ExtraIncludePaths, "/maps/Project Headers.hmap") {
		t.Fatalf("missing search paths: %#v", s)
	}
	if !slices.Contains(s.ExtraModuleMapFiles, "/maps/TikTok.modulemap") {
		t.Fatalf("missing module map: %#v", s)
	}
	if !slices.Contains(s.ExtraFrameworkPaths, "/frameworks with spaces") {
		t.Fatal(s.ExtraFrameworkPaths)
	}
	// Changing configuration must not read stale Debug compiler inputs.
	s.Configuration = "Release"
	ExtractCompilerPaths(context.Background(), s, dirs)
	if len(s.ExtraIncludePaths) != 0 {
		t.Fatal("read the wrong configuration")
	}
}

func TestRunbpResponseArguments(t *testing.T) {
	got, err := responseArguments(`-I"a b" -Xcc '-I/map name.hmap' -Iescaped\ path`)
	want := []string{"-Ia b", "-Xcc", "-I/map name.hmap", "-Iescaped path"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := responseArguments(`-I"unfinished`); err == nil {
		t.Fatal("accepted truncated response")
	}
}

func TestRunbpSettingsUseBuildCache(t *testing.T) {
	r := &fakeRunner{fetchOutput: []byte("PRODUCT_MODULE_NAME = App\nPRODUCT_BUNDLE_IDENTIFIER = test.app\nIPHONEOS_DEPLOYMENT_TARGET = 16.0\n")}
	_, err := FetchSettings(context.Background(), ProjectConfig{Project: "/app/App.xcodeproj", Scheme: "App"}, ProjectDirs{Build: "/cache with spaces/build"}, r)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(r.fetchArgs, "-derivedDataPath")
	if i < 0 || r.fetchArgs[i+1] != "/cache with spaces/build" {
		t.Fatal(r.fetchArgs)
	}
}

func TestRunbpIgnoresStaleCompilerResponse(t *testing.T) {
	s, dirs := setupRespFile(t, "-I/current/headers")
	pattern := filepath.Join(dirs.Build, "Build", "Intermediates.noindex", "*", "*", "TestModule.build", "Objects-normal", "arm64", "arguments-*.resp")
	files, _ := filepath.Glob(pattern)
	old := filepath.Join(filepath.Dir(files[0]), "arguments-000-old.resp")
	if err := os.WriteFile(old, []byte("-I/removed/package"), 0600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	ExtractCompilerPaths(context.Background(), s, dirs)
	if slices.Contains(s.ExtraIncludePaths, "/removed/package") || !slices.Contains(s.ExtraIncludePaths, "/current/headers") {
		t.Fatal(s.ExtraIncludePaths)
	}
}
