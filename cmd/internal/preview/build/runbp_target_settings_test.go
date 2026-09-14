package build

import (
	"context"
	"strings"
	"testing"
)

func TestFetchSettingsKeepsApplicationTargetTogether(t *testing.T) {
	app := `Build settings for action build and target Main App:
    PRODUCT_TYPE = com.apple.product-type.application
    PRODUCT_MODULE_NAME = MainApp
    PRODUCT_BUNDLE_IDENTIFIER = com.example.app.debug
    FULL_PRODUCT_NAME = Main App.app
    CONFIGURATION = CustomDebug
    IPHONEOS_DEPLOYMENT_TARGET = 18.0
`
	extension := `Build settings for action build and target AppExtension:
    PRODUCT_TYPE = com.apple.product-type.app-extension
    PRODUCT_MODULE_NAME = AppExtension
    PRODUCT_BUNDLE_IDENTIFIER = com.example.app.extension
    CODE_SIGN_ENTITLEMENTS = Extension.entitlements
    IPHONEOS_DEPLOYMENT_TARGET = 17.0
`
	for _, output := range []string{app + extension, extension + app} {
		settings, err := FetchSettings(context.Background(), ProjectConfig{Scheme: "Development"}, ProjectDirs{Build: t.TempDir()}, &fakeRunner{fetchOutput: []byte(output)})
		if err != nil {
			t.Fatal(err)
		}
		if settings.TargetName != "Main App" || settings.ModuleName != "MainApp" || settings.BundleID != "axe.com.example.app.debug" || settings.CodeSignEntitlements != "" || settings.DeploymentTarget != "18.0" || settings.FullProductName != "Main App.app" || settings.Configuration != "CustomDebug" {
			t.Fatalf("mixed target settings: %#v", settings)
		}
	}
	// An app's missing required value must not be supplied by its extension.
	_, err := FetchSettings(context.Background(), ProjectConfig{Scheme: "Development"}, ProjectDirs{Build: t.TempDir()}, &fakeRunner{fetchOutput: []byte(strings.ReplaceAll(app, "    PRODUCT_BUNDLE_IDENTIFIER = com.example.app.debug\n", "") + extension)})
	if err == nil || !strings.Contains(err.Error(), "PRODUCT_BUNDLE_IDENTIFIER") {
		t.Fatalf("missing app bundle ID accepted: %v", err)
	}
}

func TestSelectAppBuildSettingsRejectsAmbiguousOrNonAppTargets(t *testing.T) {
	for _, output := range []string{
		"Build settings for action build and target One:\n PRODUCT_TYPE = com.apple.product-type.application\nBuild settings for action build and target Two:\n PRODUCT_TYPE = com.apple.product-type.application\n",
		"Build settings for action build and target Extension:\n PRODUCT_TYPE = com.apple.product-type.app-extension\n",
		"Build settings for action build and target One:\n PRODUCT_MODULE_NAME = One\nBuild settings for action build and target Two:\n PRODUCT_MODULE_NAME = Two\n",
	} {
		if _, err := selectAppBuildSettings(output, "One"); err == nil {
			t.Fatalf("ambiguous or non-app settings accepted: %s", output)
		}
	}
}

func TestSelectAppBuildSettingsProductWrapperFallback(t *testing.T) {
	settings, err := selectAppBuildSettings("Build settings for action build and target Framework:\n WRAPPER_EXTENSION = framework\nBuild settings for action build and target App:\n WRAPPER_EXTENSION = app\n CODE_SIGN_ENTITLEMENTS = App.entitlements\n", "Scheme")
	if err != nil || settings["TARGET_NAME"] != "App" || settings["CODE_SIGN_ENTITLEMENTS"] != "App.entitlements" {
		t.Fatalf("%#v, %v", settings, err)
	}
}
