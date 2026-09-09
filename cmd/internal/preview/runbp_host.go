package preview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/k-kohey/axe/internal/procgroup"
	"howett.net/plist"
)

// Replace only the staged executable. The application's Debug dylib supplies
// its symbols/resources, but its SwiftUI App or UIApplicationDelegate is never
// instantiated. Source files and original build products remain untouched.
func runbpStageHost(ctx context.Context, app, deploymentTarget string) error {
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
	source := filepath.Join(filepath.Dir(app), "runbp-preview-host.m")
	if err := os.WriteFile(source, []byte(runbpHostSource), 0600); err != nil {
		return err
	}
	sdk, err := procgroup.Command(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		return err
	}
	args := []string{"clang", "-fobjc-arc", "-target", "arm64-apple-ios" + deploymentTarget + "-simulator", "-isysroot", strings.TrimSpace(string(sdk)), "-framework", "UIKit", "-framework", "Foundation", "-Xlinker", "-needed_library", "-Xlinker", library, "-Xlinker", "-rpath", "-Xlinker", "@executable_path", "-Xlinker", "-rpath", "-Xlinker", "@executable_path/Frameworks", source, "-o", filepath.Join(app, executable)}
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
