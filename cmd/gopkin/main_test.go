package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIGopkinNewScaffoldsCleanLayout(t *testing.T) {
	base := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := RunCLI([]string{"new", "-dir", base, "MyApp"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("gopkin new failed (code %d): %s", code, stderr.String())
	}

	appDir := filepath.Join(base, "MyApp")
	manifestPath := filepath.Join(appDir, "gopkin.yaml")
	mainGPKPath := filepath.Join(appDir, "app", "main.gpk")

	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected gopkin.yaml to exist: %v", err)
	}
	if _, err := os.Stat(mainGPKPath); err != nil {
		t.Fatalf("expected app/main.gpk to exist: %v", err)
	}

	// Ensure no native android/ or ios/ clutter folders were scaffolded.
	for _, forbidden := range []string{"android", "ios"} {
		if _, err := os.Stat(filepath.Join(appDir, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("did not expect %q directory in clean scaffold", forbidden)
		}
	}

	ctx, err := ResolveProject(appDir)
	if err != nil {
		t.Fatalf("ResolveProject failed on scaffolded app: %v", err)
	}
	if ctx.Manifest.Name != "MyApp" || ctx.Manifest.ID != "com.example.myapp" {
		t.Fatalf("unexpected scaffold manifest: %+v", ctx.Manifest)
	}
	if ctx.EntryPath != mainGPKPath {
		t.Fatalf("expected entry %q, got %q", mainGPKPath, ctx.EntryPath)
	}
}

func TestLegacyRootAppGPKBackwardsCompatibility(t *testing.T) {
	legacyDir := t.TempDir()
	legacyFile := filepath.Join(legacyDir, "app.gpk")
	if err := os.WriteFile(legacyFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write legacy app.gpk failed: %v", err)
	}

	ctx, err := ResolveProject(legacyDir)
	if err != nil {
		t.Fatalf("ResolveProject failed for legacy root app.gpk: %v", err)
	}
	if ctx.EntryPath != legacyFile {
		t.Fatalf("expected fallback entry %q, got %q", legacyFile, ctx.EntryPath)
	}
}

func TestParseTargetAllSupportedPlatforms(t *testing.T) {
	cases := []struct {
		in     string
		name   string
		goos   string
		goarch string
	}{
		{"", "desktop", "", ""},
		{"desktop", "desktop", "", ""},
		{"windows", "windows", "windows", "amd64"},
		{"darwin", "darwin", "darwin", "arm64"},
		{"linux", "linux", "linux", "amd64"},
		{"web", "web", "js", "wasm"},
		{"android", "android", "android", "arm64"},
		{"ios", "ios", "ios", "arm64"},
		{"iot", "iot", "linux", "arm64"},
		{"boss", "boss", "linux", "amd64"},
		{"bos", "boss", "linux", "amd64"},
	}

	for _, tc := range cases {
		spec, err := ParseTarget(tc.in)
		if err != nil {
			t.Fatalf("ParseTarget(%q) failed: %v", tc.in, err)
		}
		if spec.Name != tc.name {
			t.Errorf("ParseTarget(%q).Name = %q, want %q", tc.in, spec.Name, tc.name)
		}
		if tc.goos != "" && spec.GOOS != tc.goos {
			t.Errorf("ParseTarget(%q).GOOS = %q, want %q", tc.in, spec.GOOS, tc.goos)
		}
		if tc.goarch != "" && spec.GOARCH != tc.goarch {
			t.Errorf("ParseTarget(%q).GOARCH = %q, want %q", tc.in, spec.GOARCH, tc.goarch)
		}
	}

	if _, err := ParseTarget("playstation"); err == nil {
		t.Fatalf("expected error for unsupported target")
	}
}

func TestCLIBuildGeneratesAndroidAndIOSNativeManifests(t *testing.T) {
	base := t.TempDir()
	appDir, err := ScaffoldProject(base, "FieldScanner")
	if err != nil {
		t.Fatalf("ScaffoldProject failed: %v", err)
	}

	// Add permissions to gopkin.yaml.
	manifestYAML := `name: FieldScanner
id: com.gopkin.fieldscanner
version: 1.0.0
entry: app/main.gpk
permissions:
  location: "Used to find nearby services"
  camera: "Used to scan QR codes"
`
	if err := os.WriteFile(filepath.Join(appDir, "gopkin.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatalf("update gopkin.yaml failed: %v", err)
	}

	// Build for Android with -t android.
	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"build", "-t", "android", appDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("gopkin build -t android failed (%d): %s", code, stderr.String())
	}
	androidManifestPath := filepath.Join(appDir, "build", "android", "AndroidManifest.xml")
	androidBytes, err := os.ReadFile(androidManifestPath)
	if err != nil {
		t.Fatalf("expected AndroidManifest.xml at %s: %v", androidManifestPath, err)
	}
	androidStr := string(androidBytes)
	if !strings.Contains(androidStr, `android.permission.CAMERA`) ||
		!strings.Contains(androidStr, `android.permission.ACCESS_FINE_LOCATION`) {
		t.Fatalf("AndroidManifest.xml missing expected permissions:\n%s", androidStr)
	}

	// Build for iOS with --target ios.
	stdout.Reset()
	stderr.Reset()
	if code := RunCLI([]string{"build", "--target", "ios", appDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("gopkin build --target ios failed (%d): %s", code, stderr.String())
	}
	iosPlistPath := filepath.Join(appDir, "build", "ios", "Info.plist")
	iosBytes, err := os.ReadFile(iosPlistPath)
	if err != nil {
		t.Fatalf("expected Info.plist at %s: %v", iosPlistPath, err)
	}
	iosStr := string(iosBytes)
	if !strings.Contains(iosStr, `<key>NSCameraUsageDescription</key>`) ||
		!strings.Contains(iosStr, `<string>Used to scan QR codes</string>`) ||
		!strings.Contains(iosStr, `<key>NSLocationWhenInUseUsageDescription</key>`) ||
		!strings.Contains(iosStr, `<string>Used to find nearby services</string>`) {
		t.Fatalf("Info.plist missing expected permission keys/reasons:\n%s", iosStr)
	}

	// Run command with -t iot resolves cleanly.
	stdout.Reset()
	stderr.Reset()
	if code := RunCLI([]string{"run", "-t", "iot", appDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("gopkin run -t iot failed (%d): %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "linux/arm64") {
		t.Fatalf("expected iot run output to mention linux/arm64, got: %s", stdout.String())
	}
}

func TestCLIBuildWebTargetGeneratesHTMLAndBinary(t *testing.T) {
	base := t.TempDir()
	appDir, err := ScaffoldProject(base, "WebDemo")
	if err != nil {
		t.Fatalf("ScaffoldProject failed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"build", "-t", "web", appDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("gopkin build -t web failed (%d): %s", code, stderr.String())
	}

	indexPath := filepath.Join(appDir, "build", "web", "index.html")
	htmlBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("expected build/web/index.html: %v", err)
	}
	htmlStr := string(htmlBytes)
	if !strings.Contains(htmlStr, "<title>WebDemo</title>") || !strings.Contains(htmlStr, `<canvas id="gopkin-canvas"`) {
		t.Fatalf("unexpected build/web/index.html content:\n%s", htmlStr)
	}

	// Verify the self-contained runnable web server binary was compiled.
	entries, err := os.ReadDir(filepath.Join(appDir, "build", "web"))
	if err != nil {
		t.Fatalf("ReadDir build/web failed: %v", err)
	}
	foundBin := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "webdemo-web") {
			foundBin = true
			break
		}
	}
	if !foundBin {
		t.Fatalf("expected compiled web server binary in build/web/, found %v", entries)
	}
}

func TestCLIBuildBOSSTargetAndAlias(t *testing.T) {
	for _, targetFlag := range []string{"boss", "bos"} {
		t.Run(targetFlag, func(t *testing.T) {
			base := t.TempDir()
			appDir, err := ScaffoldProject(base, "BharatDesk")
			if err != nil {
				t.Fatalf("ScaffoldProject failed: %v", err)
			}

			var stdout, stderr bytes.Buffer
			if code := RunCLI([]string{"build", "-t", targetFlag, appDir}, &stdout, &stderr); code != 0 {
				t.Fatalf("gopkin build -t %s failed (%d): %s", targetFlag, code, stderr.String())
			}

			bossDir := filepath.Join(appDir, "build", "boss")
			binPath := filepath.Join(bossDir, "bharatdesk")
			binBytes, err := os.ReadFile(binPath)
			if err != nil {
				t.Fatalf("expected compiled BOSS binary at %s: %v", binPath, err)
			}
			// Verify static Linux ELF header (\x7fELF).
			if len(binBytes) < 4 || string(binBytes[:4]) != "\x7fELF" {
				t.Fatalf("expected Linux ELF binary at %s", binPath)
			}

			desktopPath := filepath.Join(bossDir, "bharatdesk.desktop")
			desktopBytes, err := os.ReadFile(desktopPath)
			if err != nil {
				t.Fatalf("expected .desktop launcher at %s: %v", desktopPath, err)
			}
			desktopStr := string(desktopBytes)
			if !strings.Contains(desktopStr, "[Desktop Entry]") ||
				!strings.Contains(desktopStr, "Name=BharatDesk") ||
				!strings.Contains(desktopStr, "Exec=/usr/local/bin/bharatdesk") {
				t.Fatalf("unexpected .desktop content:\n%s", desktopStr)
			}

			controlPath := filepath.Join(bossDir, "DEBIAN", "control")
			controlBytes, err := os.ReadFile(controlPath)
			if err != nil {
				t.Fatalf("expected DEBIAN/control at %s: %v", controlPath, err)
			}
			controlStr := string(controlBytes)
			if !strings.Contains(controlStr, "Package: bharatdesk") ||
				!strings.Contains(controlStr, "Architecture: amd64") ||
				!strings.Contains(controlStr, "Bharat Operating System Solutions") {
				t.Fatalf("unexpected DEBIAN/control content:\n%s", controlStr)
			}
		})
	}
}
