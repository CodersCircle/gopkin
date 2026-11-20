package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkin/core/config"
)

func TestParseManifestMapAndListPermissions(t *testing.T) {
	yamlContent := `
# Gopkin app configuration
name: SmartHome
id: com.gopkin.smarthome
version: 1.2.0
entry: app/main.gpk
permissions:
  location: "Used to find nearby services"
  camera: "Used to scan QR codes"
  bluetooth:
  notifications: "Send device alerts"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gopkin.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write gopkin.yaml failed: %v", err)
	}

	m, err := config.LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if m.Name != "SmartHome" || m.ID != "com.gopkin.smarthome" || m.Version != "1.2.0" || m.Entry != "app/main.gpk" {
		t.Fatalf("unexpected manifest metadata: %+v", m)
	}
	if m.Permissions["location"] != "Used to find nearby services" {
		t.Fatalf("unexpected location reason: %q", m.Permissions["location"])
	}
	if m.Permissions["camera"] != "Used to scan QR codes" {
		t.Fatalf("unexpected camera reason: %q", m.Permissions["camera"])
	}
	if m.Permissions["bluetooth"] == "" {
		t.Fatalf("expected default reason for bare bluetooth permission")
	}

	// List-style permissions must also parse cleanly.
	listYAML := `
name: FieldApp
id: com.example.fieldapp
permissions:
  - storage
  - camera
`
	m2, err := config.ParseManifest(listYAML)
	if err != nil {
		t.Fatalf("ParseManifest (list) failed: %v", err)
	}
	if m2.Entry != "app/main.gpk" {
		t.Fatalf("expected default entry app/main.gpk, got %q", m2.Entry)
	}
	if _, ok := m2.Permissions["storage"]; !ok {
		t.Fatalf("expected storage permission in %+v", m2.Permissions)
	}
	if _, ok := m2.Permissions["camera"]; !ok {
		t.Fatalf("expected camera permission in %+v", m2.Permissions)
	}
}

func TestManifestValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"missing name", "id: com.example.app\n"},
		{"invalid id", "name: MyApp\nid: invalid_id\n"},
		{"unknown permission", "name: MyApp\nid: com.example.app\npermissions:\n  teleport: \"Beam me up\"\n"},
	}
	for _, tc := range cases {
		if _, err := config.ParseManifest(tc.yaml); err == nil {
			t.Errorf("case %q: expected validation error, got nil", tc.name)
		}
	}
}

func TestDefaultManifestAndMarshalRoundTrip(t *testing.T) {
	m := config.DefaultManifest("My Cool App")
	if m.ID != "com.example.mycoolapp" || m.Entry != "app/main.gpk" {
		t.Fatalf("unexpected default manifest: %+v", m)
	}
	m.Permissions["camera"] = "Used to scan QR codes"

	raw := config.MarshalManifest(m)
	parsed, err := config.ParseManifest(raw)
	if err != nil {
		t.Fatalf("ParseManifest(MarshalManifest) failed: %v\nYAML:\n%s", err, raw)
	}
	if parsed.Name != m.Name || parsed.ID != m.ID || parsed.Permissions["camera"] != "Used to scan QR codes" {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", parsed, m)
	}
}

func TestGenerateAndroidManifestAndIOSInfoPlist(t *testing.T) {
	m := config.Manifest{
		Name:    "Gopkin Tracker",
		ID:      "com.gopkin.tracker",
		Version: "2.0.1",
		Entry:   "app/main.gpk",
		Permissions: map[string]string{
			"location":      "Used to find nearby services",
			"camera":        "Used to scan QR codes",
			"bluetooth":     "Connect to smart sensors",
			"notifications": "Deliver alert notifications",
			"storage":       "Save offline reports",
		},
	}

	androidXML := config.GenerateAndroidManifest(m)
	wantAndroidSnippets := []string{
		`package="com.gopkin.tracker"`,
		`android:versionName="2.0.1"`,
		`android:label="Gopkin Tracker"`,
		`<uses-permission android:name="android.permission.ACCESS_FINE_LOCATION" />`,
		`<uses-permission android:name="android.permission.ACCESS_COARSE_LOCATION" />`,
		`<uses-permission android:name="android.permission.CAMERA" />`,
		`<uses-permission android:name="android.permission.BLUETOOTH_CONNECT" />`,
		`<uses-permission android:name="android.permission.BLUETOOTH_SCAN" />`,
		`<uses-permission android:name="android.permission.POST_NOTIFICATIONS" />`,
		`<uses-permission android:name="android.permission.READ_EXTERNAL_STORAGE" />`,
		`<uses-permission android:name="android.permission.WRITE_EXTERNAL_STORAGE" />`,
	}
	for _, snip := range wantAndroidSnippets {
		if !strings.Contains(androidXML, snip) {
			t.Errorf("AndroidManifest.xml missing %q\nGot:\n%s", snip, androidXML)
		}
	}

	iosPlist := config.GenerateIOSInfoPlist(m)
	wantIOSSnippets := []string{
		`<key>CFBundleIdentifier</key>`,
		`<string>com.gopkin.tracker</string>`,
		`<key>CFBundleDisplayName</key>`,
		`<string>Gopkin Tracker</string>`,
		`<key>NSLocationWhenInUseUsageDescription</key>`,
		`<string>Used to find nearby services</string>`,
		`<key>NSCameraUsageDescription</key>`,
		`<string>Used to scan QR codes</string>`,
		`<key>NSBluetoothAlwaysUsageDescription</key>`,
		`<string>Connect to smart sensors</string>`,
		`<key>NSUserNotificationsUsageDescription</key>`,
		`<string>Deliver alert notifications</string>`,
		`<key>NSPhotoLibraryUsageDescription</key>`,
		`<string>Save offline reports</string>`,
	}
	for _, snip := range wantIOSSnippets {
		if !strings.Contains(iosPlist, snip) {
			t.Errorf("Info.plist missing %q\nGot:\n%s", snip, iosPlist)
		}
	}
}
