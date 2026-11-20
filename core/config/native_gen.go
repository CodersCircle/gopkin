package config

import (
	"fmt"
	"sort"
	"strings"
)

var androidPermissionMap = map[string][]string{
	"location": {
		"android.permission.ACCESS_FINE_LOCATION",
		"android.permission.ACCESS_COARSE_LOCATION",
	},
	"camera": {
		"android.permission.CAMERA",
	},
	"bluetooth": {
		"android.permission.BLUETOOTH_CONNECT",
		"android.permission.BLUETOOTH_SCAN",
	},
	"notifications": {
		"android.permission.POST_NOTIFICATIONS",
	},
	"storage": {
		"android.permission.READ_EXTERNAL_STORAGE",
		"android.permission.WRITE_EXTERNAL_STORAGE",
	},
}

var iosPlistKeyMap = map[string]string{
	"location":      "NSLocationWhenInUseUsageDescription",
	"camera":        "NSCameraUsageDescription",
	"bluetooth":     "NSBluetoothAlwaysUsageDescription",
	"notifications": "NSUserNotificationsUsageDescription",
	"storage":       "NSPhotoLibraryUsageDescription",
}

// GenerateAndroidManifest produces a build-time AndroidManifest.xml with
// <uses-permission> entries automatically injected from m.Permissions.
func GenerateAndroidManifest(m Manifest) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(fmt.Sprintf(
		`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="%s" android:versionName="%s">`+"\n",
		xmlEscape(m.ID),
		xmlEscape(fallbackVersion(m.Version)),
	))

	b.WriteString(`    <uses-permission android:name="android.permission.INTERNET" />` + "\n")

	keys := sortedPermissionKeys(m.Permissions)
	for _, perm := range keys {
		for _, androidPerm := range androidPermissionMap[perm] {
			b.WriteString(fmt.Sprintf(`    <uses-permission android:name="%s" />`+"\n", androidPerm))
		}
	}

	b.WriteString(fmt.Sprintf(`    <application android:label="%s">`+"\n", xmlEscape(m.Name)))
	b.WriteString(`        <activity android:name=".MainActivity" android:exported="true">` + "\n")
	b.WriteString(`            <intent-filter>` + "\n")
	b.WriteString(`                <action android:name="android.intent.action.MAIN" />` + "\n")
	b.WriteString(`                <category android:name="android.intent.category.LAUNCHER" />` + "\n")
	b.WriteString(`            </intent-filter>` + "\n")
	b.WriteString(`        </activity>` + "\n")
	b.WriteString(`    </application>` + "\n")
	b.WriteString(`</manifest>` + "\n")
	return b.String()
}

// GenerateIOSInfoPlist produces a build-time iOS Info.plist with Apple privacy
// usage description keys populated from m.Permissions.
func GenerateIOSInfoPlist(m Manifest) string {
	version := fallbackVersion(m.Version)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString(`<dict>` + "\n")
	writePlistString(&b, "CFBundleIdentifier", m.ID)
	writePlistString(&b, "CFBundleName", m.Name)
	writePlistString(&b, "CFBundleDisplayName", m.Name)
	writePlistString(&b, "CFBundleShortVersionString", version)
	writePlistString(&b, "CFBundleVersion", version)
	b.WriteString("    <key>LSRequiresIPhoneOS</key>\n    <true/>\n")

	keys := sortedPermissionKeys(m.Permissions)
	for _, perm := range keys {
		plistKey, ok := iosPlistKeyMap[perm]
		if !ok {
			continue
		}
		reason := m.Permissions[perm]
		if strings.TrimSpace(reason) == "" {
			reason = defaultPermissionReason(perm, m.Name)
		}
		writePlistString(&b, plistKey, reason)
	}

	b.WriteString(`</dict>` + "\n")
	b.WriteString(`</plist>` + "\n")
	return b.String()
}

func writePlistString(b *strings.Builder, key, val string) {
	b.WriteString("    <key>" + xmlEscape(key) + "</key>\n")
	b.WriteString("    <string>" + xmlEscape(val) + "</string>\n")
}

func sortedPermissionKeys(perms map[string]string) []string {
	keys := make([]string, 0, len(perms))
	for k := range perms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fallbackVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "0.1.0"
	}
	return v
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// GenerateBOSSDesktopEntry generates a Debian/BOSS Linux freedesktop .desktop
// launcher file for Bharat Operating System Solutions (BOSS).
func GenerateBOSSDesktopEntry(m Manifest, binaryName string) string {
	bin := strings.TrimSpace(binaryName)
	if bin == "" {
		bin = sanitizeAppIDSegment(m.Name)
	}
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=" + fallbackVersion(m.Version) + "\n")
	b.WriteString("Name=" + m.Name + "\n")
	b.WriteString("Comment=" + m.Name + " — Built with Gopkin for BOSS Linux\n")
	b.WriteString("Exec=/usr/local/bin/" + bin + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Utility;Application;\n")
	return b.String()
}

// GenerateBOSSDebianControl generates a DEBIAN/control package metadata file
// compatible with BOSS (Bharat Operating System Solutions) / Debian dpkg.
func GenerateBOSSDebianControl(m Manifest, packageName string) string {
	pkg := strings.ToLower(strings.TrimSpace(packageName))
	if pkg == "" {
		pkg = sanitizeAppIDSegment(m.Name)
	}
	var b strings.Builder
	b.WriteString("Package: " + pkg + "\n")
	b.WriteString("Version: " + fallbackVersion(m.Version) + "\n")
	b.WriteString("Section: utils\n")
	b.WriteString("Priority: optional\n")
	b.WriteString("Architecture: amd64\n")
	b.WriteString("Maintainer: " + m.Name + " Maintainers <noreply@" + m.ID + ">\n")
	b.WriteString("Description: " + m.Name + " (Gopkin Pure-Go Application for BOSS Linux)\n")
	b.WriteString(" Static zero-CGO application packaged for Bharat Operating System Solutions.\n")
	return b.String()
}
