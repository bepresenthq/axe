package build

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/k-kohey/axe/internal/preview/buildlock"
)

// Result holds the output of a Prepare call.
type Result struct {
	Settings *Settings
	Dirs     ProjectDirs
	Built    bool // true if xcodebuild was invoked (false when reusing a previous build)
}

// Prepare runs the full build pipeline: fetch settings, optionally build,
// and extract compiler paths. This is the high-level entry point.
func Prepare(ctx context.Context, pc ProjectConfig, dirs ProjectDirs, reuse bool, r Runner) (*Result, error) {
	s, err := FetchSettings(ctx, pc, dirs, r)
	if err != nil {
		return nil, err
	}

	built := false
	if reuse && HasPreviousBuild(s, dirs) {
		slog.Info("Reusing previous build", "buildDir", dirs.Build)
	} else {
		if err := Run(ctx, pc, dirs, r); err != nil {
			return nil, err
		}
		built = true
	}

	ExtractCompilerPaths(ctx, s, dirs)

	return &Result{Settings: s, Dirs: dirs, Built: built}, nil
}

// runbp's native host and thunk compiler require Apple Silicon. Use the same
// settings for discovery and compilation so neither builds Intel products or
// resolves a different package graph for the preview.
func previewBuildArgs(dirs ProjectDirs) []string {
	return []string{
		"-destination", "generic/platform=iOS Simulator",
		"-derivedDataPath", dirs.Build,
		"ARCHS=arm64", "ONLY_ACTIVE_ARCH=YES",
		"ENABLE_DEBUG_DYLIB=YES",
		"OTHER_SWIFT_FLAGS=$(inherited) -Xfrontend -enable-implicit-dynamic -Xfrontend -enable-private-imports",
	}
}

// FetchSettings runs "xcodebuild -showBuildSettings" and parses the output
// into a Settings struct.
func FetchSettings(ctx context.Context, pc ProjectConfig, dirs ProjectDirs, r Runner) (*Settings, error) {
	args := append(
		[]string{"xcodebuild", "-showBuildSettings"},
		pc.XcodebuildArgs()...,
	)
	args = append(args, previewBuildArgs(dirs)...)

	out, err := r.FetchBuildSettings(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("xcodebuild -showBuildSettings failed: %w\n%s", err, out)
	}

	keys, err := selectAppBuildSettings(string(out), pc.Scheme)
	if err != nil {
		return nil, err
	}

	config := pc.Configuration
	if config == "" {
		config = keys["CONFIGURATION"]
	}
	if config == "" {
		config = "Debug"
	}
	builtProductsDir := filepath.Join(dirs.Build, "Build", "Products", config+"-iphonesimulator")

	s := &Settings{
		ModuleName:           keys["PRODUCT_MODULE_NAME"],
		TargetName:           keys["TARGET_NAME"],
		FullProductName:      keys["FULL_PRODUCT_NAME"],
		Configuration:        config,
		BundleID:             "axe." + keys["PRODUCT_BUNDLE_IDENTIFIER"],
		OriginalBundleID:     keys["PRODUCT_BUNDLE_IDENTIFIER"],
		BuiltProductsDir:     builtProductsDir,
		DeploymentTarget:     keys["IPHONEOS_DEPLOYMENT_TARGET"],
		SwiftVersion:         keys["SWIFT_VERSION"],
		CodeSignEntitlements: keys["CODE_SIGN_ENTITLEMENTS"],
	}

	if s.ModuleName == "" {
		return nil, fmt.Errorf("PRODUCT_MODULE_NAME not found in build settings")
	}
	if s.OriginalBundleID == "" {
		return nil, fmt.Errorf("PRODUCT_BUNDLE_IDENTIFIER not found in build settings")
	}
	if s.DeploymentTarget == "" {
		return nil, fmt.Errorf("IPHONEOS_DEPLOYMENT_TARGET not found in build settings")
	}

	slog.Debug("Build settings",
		"module", s.ModuleName,
		"bundle", s.BundleID,
		"products", s.BuiltProductsDir,
		"target", s.DeploymentTarget,
		"swiftVersion", s.SwiftVersion,
	)
	return s, nil
}

// Select an entire app target block before reading any setting. A scheme may
// also emit extensions, frameworks and test targets, each with different IDs
// and entitlements. Headerless single-target output remains supported.
func selectAppBuildSettings(output, scheme string) (map[string]string, error) {
	var blocks []map[string]string
	current := map[string]string{}
	flush := func() {
		if len(current) > 0 {
			blocks = append(blocks, current)
		}
		current = map[string]string{}
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Build settings for action ") && strings.Contains(line, " and target ") && strings.HasSuffix(line, ":") {
			flush()
			_, name, _ := strings.Cut(line, " and target ")
			current["TARGET_NAME"] = strings.TrimSuffix(name, ":")
			continue
		}
		if key, value, ok := strings.Cut(line, " ="); ok && key != "" && !strings.ContainsAny(key, " \t") {
			current[key] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading build settings: %w", err)
	}
	flush()
	var apps []map[string]string
	for _, block := range blocks {
		productType := block["PRODUCT_TYPE"]
		if productType == "com.apple.product-type.application" ||
			(productType == "" && (block["WRAPPER_EXTENSION"] == "app" || strings.HasSuffix(block["FULL_PRODUCT_NAME"], ".app"))) {
			apps = append(apps, block)
		}
	}
	if len(apps) == 1 {
		return apps[0], nil
	}
	// Multiple application products are ambiguous even when one happens to
	// share the scheme's name: scheme names do not identify the runnable target.
	if len(apps) > 1 {
		return nil, fmt.Errorf("scheme %q has multiple application targets in build settings; choose a scheme with one runnable iOS app", scheme)
	}
	if len(blocks) == 0 {
		return map[string]string{}, nil
	}
	if len(blocks) == 1 && blocks[0]["PRODUCT_TYPE"] == "" && blocks[0]["WRAPPER_EXTENSION"] == "" {
		return blocks[0], nil
	}
	return nil, fmt.Errorf("scheme %q does not identify a single application target in build settings", scheme)
}

// Run executes "xcodebuild build" with the flags required for axe preview
// (dynamic replacement and private imports).
func Run(ctx context.Context, pc ProjectConfig, dirs ProjectDirs, r Runner) error {
	lock := buildlock.New(dirs.Build)
	if err := lock.Lock(ctx); err != nil {
		return fmt.Errorf("acquiring build lock: %w", err)
	}
	defer lock.Unlock()

	args := append(
		[]string{"xcodebuild", "build"},
		pc.XcodebuildArgs()...,
	)
	args = append(args, previewBuildArgs(dirs)...)

	out, err := r.Build(ctx, args)
	if err != nil {
		return fmt.Errorf("xcodebuild build failed: %w\n%s", err, out)
	}

	return nil
}

// ExtractCompilerPaths reads the swiftc response file (.resp) generated
// during the xcodebuild build and extracts -I, -F, and -fmodule-map-file=
// flags. These are required so that the thunk compilation can resolve
// transitive SPM dependencies (C module headers, framework bundles, and
// generated ObjC module maps) that xcodebuild manages internally.
func ExtractCompilerPaths(ctx context.Context, s *Settings, dirs ProjectDirs) {
	// Clear previously extracted paths so that re-extraction after a rebuild
	// produces fresh results (idempotent).
	s.ExtraIncludePaths = nil
	s.ExtraFrameworkPaths = nil
	s.ExtraModuleMapFiles = nil

	lock := buildlock.New(dirs.Build)
	if err := lock.RLock(ctx); err != nil {
		slog.Warn("Failed to acquire read lock for compiler paths", "err", err)
		return
	}
	defer lock.RUnlock()
	target := s.TargetName
	if target == "" {
		target = s.ModuleName
	}
	configuration := s.Configuration
	if configuration == "" {
		configuration = "Debug"
	}
	targetPattern := filepath.Join(dirs.Build, "Build", "Intermediates.noindex", "*", configuration+"-iphonesimulator", target+".build")
	objects := filepath.Join(targetPattern, "Objects-normal", "arm64")
	// New Xcode versions put Clang search paths in common-args response files,
	// while Swift explicit dependency manifests carry the module maps. Read both.
	deps, _ := filepath.Glob(filepath.Join(objects, "*-dependencies-*.json"))
	if len(deps) > 0 {
		if err := extractCompilerPathsFromDependencies(s, dirs.Build, newestCompilerFile(deps)); err != nil {
			slog.Warn("Failed to extract compiler paths", "err", err)
		}
	}
	for _, pattern := range []string{"arguments-*.resp", "*-common-args.resp"} {
		matches, _ := filepath.Glob(filepath.Join(objects, pattern))
		if len(matches) == 0 {
			continue
		}
		for _, file := range []string{newestCompilerFile(matches)} {
			data, err := os.ReadFile(file)
			if err != nil {
				slog.Warn("Failed to read compiler response", "path", file, "err", err)
				continue
			}
			extractCompilerPathsFromResp(s, string(data))
		}
	}
	// Swift-only targets may not emit a Clang response file. Their header maps
	// still belong to the target, and are needed by serialized bridging headers.
	maps, _ := filepath.Glob(filepath.Join(targetPattern, s.ModuleName+"-*.hmap"))
	for _, file := range maps {
		s.ExtraIncludePaths = appendUnique(s.ExtraIncludePaths, file)
	}
}

func extractCompilerPathsFromResp(s *Settings, data string) {
	args, err := responseArguments(data)
	if err != nil {
		slog.Warn("Invalid compiler response file", "err", err)
		return
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if p, ok := strings.CutPrefix(arg, "-fmodule-map-file="); ok {
			s.ExtraModuleMapFiles = appendUnique(s.ExtraModuleMapFiles, p)
			continue
		}
		for _, flag := range []string{"-iquote", "-isystem", "-I", "-F"} {
			p, ok := strings.CutPrefix(arg, flag)
			if !ok {
				continue
			}
			if p == "" && i+1 < len(args) {
				i++
				p = args[i]
			}
			if p == "" || p == s.BuiltProductsDir {
				break
			}
			if flag == "-F" {
				s.ExtraFrameworkPaths = appendUnique(s.ExtraFrameworkPaths, p)
			} else {
				s.ExtraIncludePaths = appendUnique(s.ExtraIncludePaths, p)
			}
			break
		}
	}

	slog.Debug("Extracted paths from resp file",
		"includePaths", len(s.ExtraIncludePaths),
		"frameworkPaths", len(s.ExtraFrameworkPaths),
		"moduleMapFiles", len(s.ExtraModuleMapFiles),
	)
}

type dependencyManifestEntry struct {
	ClangModuleMapPath string `json:"clangModuleMapPath"`
}

var umbrellaDirectiveRE = regexp.MustCompile(`^(umbrella header|umbrella)\s+"([^"]+)"`)

func extractCompilerPathsFromDependencies(s *Settings, buildDir, manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	var entries []dependencyManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parsing dependency manifest: %w", err)
	}

	seenI := map[string]bool{s.BuiltProductsDir: true}
	seenF := map[string]bool{s.BuiltProductsDir: true}
	seenM := map[string]bool{}

	addIncludePath := func(path string) {
		path = filepath.Clean(path)
		if path == "" || path == "." || path == "/" || seenI[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seenI[path] = true
		s.ExtraIncludePaths = append(s.ExtraIncludePaths, path)
	}
	addFrameworkPath := func(path string) {
		path = filepath.Clean(path)
		if path == "" || path == "." || path == "/" || seenF[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seenF[path] = true
		s.ExtraFrameworkPaths = append(s.ExtraFrameworkPaths, path)
	}
	addModuleMapPath := func(path string) {
		path = filepath.Clean(path)
		if path == "" || path == "." || seenM[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seenM[path] = true
		s.ExtraModuleMapFiles = append(s.ExtraModuleMapFiles, path)
	}

	// Only process module maps that belong to this build (under buildDir).
	// SDK module maps (e.g. UIKit, Foundation) are resolved by swiftc via -sdk
	// and must not be added explicitly — doing so bloats the compiler flags and
	// can cause swiftc to hang.
	isOwnedByBuild := func(path string) bool {
		rel, err := filepath.Rel(filepath.Clean(buildDir), filepath.Clean(path))
		if err != nil {
			return false
		}
		return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}

	generatedModuleMapsDir := filepath.Join(buildDir, "Build", "Intermediates.noindex", "GeneratedModuleMaps-iphonesimulator")
	addIncludePath(generatedModuleMapsDir)

	for _, entry := range entries {
		if entry.ClangModuleMapPath == "" {
			continue
		}

		moduleMapPath := filepath.Clean(entry.ClangModuleMapPath)
		if !isOwnedByBuild(moduleMapPath) {
			continue
		}

		addModuleMapPath(moduleMapPath)

		// For framework-style module maps, add -F for the framework's parent.
		// Versioned frameworks or non-standard module map names are not covered here.
		if strings.HasSuffix(moduleMapPath, "/Modules/module.modulemap") {
			for current := filepath.Dir(moduleMapPath); current != "/" && current != "."; current = filepath.Dir(current) {
				if strings.HasSuffix(current, ".framework") {
					addFrameworkPath(filepath.Dir(current))
					break
				}
			}
			continue
		}

		// For non-framework module maps, add the umbrella header/directory as -I.
		moduleMapData, err := os.ReadFile(moduleMapPath)
		if err != nil {
			slog.Debug("Failed to read module map file", "path", moduleMapPath, "err", err)
			continue
		}
		for line := range strings.SplitSeq(string(moduleMapData), "\n") {
			m := umbrellaDirectiveRE.FindStringSubmatch(strings.TrimSpace(line))
			if len(m) != 3 {
				continue
			}

			targetPath := m[2]
			if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(filepath.Dir(moduleMapPath), targetPath)
			}
			targetPath = filepath.Clean(targetPath)
			info, err := os.Stat(targetPath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				addIncludePath(targetPath)
			} else {
				addIncludePath(filepath.Dir(targetPath))
			}
		}
	}

	slog.Debug("Extracted paths from dependency manifest",
		"path", manifestPath,
		"includePaths", len(s.ExtraIncludePaths),
		"frameworkPaths", len(s.ExtraFrameworkPaths),
		"moduleMapFiles", len(s.ExtraModuleMapFiles),
	)
	return nil
}

// HasPreviousBuild checks whether a .app bundle exists in the build products
// directory, indicating that a previous build can be reused.
func HasPreviousBuild(s *Settings, dirs ProjectDirs) bool {
	appName := s.FullProductName
	if appName == "" {
		appName = s.ModuleName + ".app"
	}
	primaryPath := filepath.Join(s.BuiltProductsDir, appName)
	if _, err := os.Stat(primaryPath); err == nil {
		return true
	}
	pattern := filepath.Join(dirs.Build, "Build", "Products", "*", appName)
	matches, _ := filepath.Glob(pattern)
	return len(matches) > 0
}

// Old response/manifests can survive incremental builds after package or branch
// changes. Use the last compiler invocation, never merge stale search paths.
func newestCompilerFile(files []string) string {
	newest := files[0]
	for _, file := range files[1:] {
		a, errA := os.Stat(file)
		b, errB := os.Stat(newest)
		if errA == nil && (errB != nil || a.ModTime().After(b.ModTime())) {
			newest = file
		}
	}
	return newest
}
