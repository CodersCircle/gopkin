package config

import (
	"fmt"
	"path"
	"strings"
)

// AndroidPackagePath converts a reverse-DNS bundle ID (e.g. "com.example.myapp")
// into a slash-separated Java/Kotlin source path ("com/example/myapp").
func AndroidPackagePath(bundleID string) string {
	parts := strings.Split(strings.TrimSpace(bundleID), ".")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		return "com/example/app"
	}
	return path.Join(clean...)
}

// GenerateAndroidBuildGradle produces a clean app/build.gradle configured with
// applicationId and versionName from gopkin.yaml.
func GenerateAndroidBuildGradle(m Manifest) string {
	version := fallbackVersion(m.Version)
	return fmt.Sprintf(`plugins {
    id 'com.android.application'
    id 'org.jetbrains.kotlin.android'
}

android {
    namespace '%s'
    compileSdk 34

    defaultConfig {
        applicationId "%s"
        minSdk 24
        targetSdk 34
        versionCode 1
        versionName "%s"
    }

    buildTypes {
        release {
            minifyEnabled false
        }
    }

    compileOptions {
        sourceCompatibility JavaVersion.VERSION_17
        targetCompatibility JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = '17'
    }
}

dependencies {
    implementation 'androidx.core:core-ktx:1.13.1'
    implementation 'androidx.appcompat:appcompat:1.7.0'
}
`, m.ID, m.ID, version)
}

// GenerateAndroidMainActivity produces a minimal Kotlin Activity hosting the
// Gopkin runtime SurfaceView and bridging runtime permission requests declared
// in gopkin.yaml.
func GenerateAndroidMainActivity(m Manifest) string {
	keys := sortedPermissionKeys(m.Permissions)
	var permLiterals []string
	for _, k := range keys {
		for _, ap := range androidPermissionMap[k] {
			permLiterals = append(permLiterals, fmt.Sprintf(`        "%s"`, ap))
		}
	}
	permsBlock := ""
	if len(permLiterals) > 0 {
		permsBlock = strings.Join(permLiterals, ",\n")
	}

	return fmt.Sprintf(`package %s

import android.content.pm.PackageManager
import android.os.Bundle
import android.view.SurfaceHolder
import android.view.SurfaceView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat

class MainActivity : AppCompatActivity(), SurfaceHolder.Callback {

    private val declaredPermissions = arrayOf(
%s
    )

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = %q

        val surfaceView = SurfaceView(this)
        surfaceView.holder.addCallback(this)
        setContentView(surfaceView)

        requestDeclaredPermissionsIfNeeded()
    }

    private fun requestDeclaredPermissionsIfNeeded() {
        val missing = declaredPermissions.filter { perm ->
            ContextCompat.checkSelfPermission(this, perm) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            ActivityCompat.requestPermissions(this, missing.toTypedArray(), 1001)
        }
    }

    override fun surfaceCreated(holder: SurfaceHolder) {
        // Attach Gopkin Pure-Go runtime surface here.
    }

    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
        // Notify Gopkin runtime of logical viewport resize.
    }

    override fun surfaceDestroyed(holder: SurfaceHolder) {
        // Pause Gopkin render surface cleanly.
    }
}
`, m.ID, permsBlock, m.Name)
}

// GenerateIOSAppSwift produces a minimal SwiftUI/UIKit host entry point
// (Sources/GopkinApp.swift) that hosts the Gopkin runtime surface.
func GenerateIOSAppSwift(m Manifest) string {
	return fmt.Sprintf(`import SwiftUI
import UIKit

@main
struct GopkinHostApp: App {
    var body: some Scene {
        WindowGroup {
            GopkinSurfaceHostView(appTitle: %q)
                .ignoresSafeArea()
        }
    }
}

struct GopkinSurfaceHostView: UIViewRepresentable {
    let appTitle: String

    func makeUIView(context: Context) -> UIView {
        let view = UIView(frame: .zero)
        view.backgroundColor = .systemBackground
        view.accessibilityLabel = appTitle
        return view
    }

    func updateUIView(_ uiView: UIView, context: Context) {
        // Synchronize Gopkin runtime frame updates with the native UIView surface.
    }
}
`, m.Name)
}
