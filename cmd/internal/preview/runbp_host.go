package preview

import (
	"context"
	"debug/macho"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kohey/axe/internal/procgroup"
	"howett.net/plist"
)

// Replace only the staged executable. The application's Debug dylib supplies
// its symbols/resources, but its SwiftUI App or UIApplicationDelegate is never
// instantiated. Source files and original build products remain untouched.
func runbpStageHost(ctx context.Context, app, deploymentTarget string, declaredEntitlements string) error {
	infoPath := filepath.Join(app, "Info.plist")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return err
	}
	var info map[string]any
	if _, err := plist.Unmarshal(data, &info); err != nil {
		return err
	}
	executable, ok := info["CFBundleExecutable"].(string)
	if !ok || filepath.Base(executable) != executable {
		return fmt.Errorf("invalid preview app executable")
	}
	library := filepath.Join(app, executable+".debug.dylib")
	if _, err := os.Stat(library); err != nil {
		return fmt.Errorf("preview needs Xcode's Debug dylib; ENABLE_DEBUG_DYLIB=YES did not produce %s", library)
	}
	entitlements, err := runbpHostEntitlements(filepath.Join(app, executable), declaredEntitlements)
	if err != nil {
		return err
	}
	entitlementsPath := filepath.Join(filepath.Dir(app), "runbp-preview-entitlements.plist")
	if err := os.WriteFile(entitlementsPath, entitlements, 0600); err != nil {
		return err
	}
	source := filepath.Join(filepath.Dir(app), "runbp-preview-host.m")
	if err := os.WriteFile(source, []byte(runbpHostSource), 0600); err != nil {
		return err
	}
	sdk, err := procgroup.Command(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		return err
	}
	args := []string{"clang", "-fobjc-arc", "-target", "arm64-apple-ios" + deploymentTarget + "-simulator", "-isysroot", strings.TrimSpace(string(sdk)), "-framework", "UIKit", "-framework", "Foundation", "-Xlinker", "-needed_library", "-Xlinker", library, "-Xlinker", "-rpath", "-Xlinker", "@executable_path", "-Xlinker", "-rpath", "-Xlinker", "@executable_path/Frameworks", source, "-o", filepath.Join(app, executable)}
	args = append(args, "-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", entitlementsPath)
	if out, err := procgroup.Command(ctx, "xcrun", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("compiling preview host: %w\n%s", err, out)
	}
	info["UILaunchScreen"] = map[string]any{}
	delete(info, "UIMainStoryboardFile")
	delete(info, "UIMainStoryboardFile~ipad")
	info["UIApplicationSceneManifest"] = map[string]any{"UIApplicationSupportsMultipleScenes": false, "UISceneConfigurations": map[string]any{"UIWindowSceneSessionRoleApplication": []any{map[string]any{"UISceneConfigurationName": "runbp", "UISceneDelegateClassName": "RunbpPreviewScene"}}}}
	data, err = plist.Marshal(info, plist.XMLFormat)
	if err != nil {
		return err
	}
	if err := os.WriteFile(infoPath, data, 0600); err != nil {
		return err
	}
	if out, err := procgroup.Command(ctx, "codesign", "--force", "--sign", "-", "--deep", app).CombinedOutput(); err != nil {
		return fmt.Errorf("signing preview host: %w\n%s", err, out)
	}
	return nil
}

// Simulator capabilities live in the Mach-O section, not the ad-hoc signature.
// Read the staged Xcode executable before replacing it, so substitutions and
// configuration-specific entitlements have already been resolved by Xcode.
func runbpHostEntitlements(executable, declared string) ([]byte, error) {
	file, err := macho.Open(executable)
	if err != nil {
		return nil, fmt.Errorf("reading preview simulator entitlements: %w", err)
	}
	defer file.Close()
	var data []byte
	for _, section := range file.Sections {
		if section.Seg == "__TEXT" && section.Name == "__entitlements" {
			data, err = section.Data()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	return runbpSimulatorEntitlements(data, declared)
}

func runbpSimulatorEntitlements(data []byte, declared string) ([]byte, error) {
	entitlements := map[string]any{}
	if len(data) > 0 {
		if _, err := plist.Unmarshal(data, &entitlements); err != nil {
			return nil, fmt.Errorf("invalid simulator entitlements: %w", err)
		}
	} else if declared != "" {
		slog.Warn("Preview executable has no embedded simulator entitlements; declared capabilities are unavailable in this host. This is expected when simulator code signing is disabled. Preview views that do not require those capabilities, or verify capability-dependent flows through the normally signed full app; no application checkout changes are required", "declaredEntitlements", declared)
	}
	entitlements["get-task-allow"] = true
	for key := range entitlements {
		if key != "get-task-allow" && key != "com.apple.security.application-groups" {
			slog.Warn(runbpCapabilityDiagnostic(key), "entitlement", key)
		}
	}
	return plist.Marshal(entitlements, plist.XMLFormat)
}

func runbpCapabilityDiagnostic(key string) string {
	switch key {
	case "com.apple.developer.family-controls":
		return "Preview cannot verify Screen Time authorization or enforcement; use runbp up on a provisioned physical device"
	case "aps-environment":
		return "Preview skips application-delegate push registration; verify notification setup and delivery through the full app"
	case "application-identifier", "com.apple.application-identifier", "keychain-access-groups", "com.apple.developer.associated-domains":
		return "Preview changes the app bundle ID; verify identity-dependent keychain, URL and associated-domain behavior through the full app"
	default:
		return "Preview preserves simulator entitlement metadata, but service availability needs explicit account, provisioning and runtime verification through the full app"
	}
}

const runbpHostSource = `#import <UIKit/UIKit.h>
@interface RunbpPreviewScene : UIResponder <UIWindowSceneDelegate>
@property(nonatomic, strong) UIWindow *window;
@end
@implementation RunbpPreviewScene
- (void)scene:(UIScene *)scene willConnectToSession:(UISceneSession *)session options:(UISceneConnectionOptions *)options {
    if (![scene isKindOfClass:UIWindowScene.class]) return;
    self.window = [[UIWindow alloc] initWithWindowScene:(UIWindowScene *)scene];
    self.window.rootViewController = [UIViewController new];
    [self.window makeKeyAndVisible];
}
@end
@interface RunbpPreviewDelegate : UIResponder <UIApplicationDelegate>
@end
@implementation RunbpPreviewDelegate
- (BOOL)application:(UIApplication *)application didFinishLaunchingWithOptions:(NSDictionary *)options { return YES; }
@end
int main(int argc, char **argv) {
    @autoreleasepool { return UIApplicationMain(argc, argv, nil, NSStringFromClass(RunbpPreviewDelegate.class)); }
}
`
