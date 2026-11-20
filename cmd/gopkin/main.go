package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkin/core/config"
)

// TargetSpec describes a resolved Gopkin build/run target.
type TargetSpec struct {
	Name   string // canonical target name (desktop, windows, darwin, linux, web, android, ios, iot)
	GOOS   string
	GOARCH string
	Ext    string
}

// ParseTarget resolves a -t / --target value into a TargetSpec.
func ParseTarget(raw string) (TargetSpec, error) {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" || t == "desktop" {
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		return TargetSpec{
			Name:   "desktop",
			GOOS:   runtime.GOOS,
			GOARCH: runtime.GOARCH,
			Ext:    ext,
		}, nil
	}

	switch t {
	case "windows":
		return TargetSpec{Name: "windows", GOOS: "windows", GOARCH: "amd64", Ext: ".exe"}, nil
	case "darwin", "macos":
		return TargetSpec{Name: "darwin", GOOS: "darwin", GOARCH: "arm64"}, nil
	case "linux":
		return TargetSpec{Name: "linux", GOOS: "linux", GOARCH: "amd64"}, nil
	case "web":
		return TargetSpec{Name: "web", GOOS: "js", GOARCH: "wasm", Ext: ".wasm"}, nil
	case "android":
		return TargetSpec{Name: "android", GOOS: "android", GOARCH: "arm64"}, nil
	case "ios":
		return TargetSpec{Name: "ios", GOOS: "ios", GOARCH: "arm64"}, nil
	case "iot":
		return TargetSpec{Name: "iot", GOOS: "linux", GOARCH: "arm64"}, nil
	default:
		return TargetSpec{}, fmt.Errorf("unsupported target %q (supported: desktop, windows, darwin, linux, web, android, ios, iot)", raw)
	}
}

// ProjectContext holds a resolved Gopkin project manifest and entry file path.
type ProjectContext struct {
	RootDir   string
	Manifest  config.Manifest
	EntryPath string
}

// ResolveProject loads gopkin.yaml (if present) and locates the app entry file.
// Supports the clean Flutter-inspired `app/main.gpk` layout and falls back to
// legacy root `app.gpk` for backwards compatibility.
func ResolveProject(projectDir string) (ProjectContext, error) {
	if strings.TrimSpace(projectDir) == "" {
		projectDir = "."
	}
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return ProjectContext{}, err
	}

	manifestPath := filepath.Join(absDir, "gopkin.yaml")
	var manifest config.Manifest
	if _, statErr := os.Stat(manifestPath); statErr == nil {
		manifest, err = config.LoadManifest(manifestPath)
		if err != nil {
			return ProjectContext{}, err
		}
	} else {
		manifest = config.DefaultManifest(filepath.Base(absDir))
	}

	// Primary entry path from manifest (default: app/main.gpk).
	primaryEntry := filepath.Join(absDir, filepath.FromSlash(manifest.Entry))
	if _, err := os.Stat(primaryEntry); err == nil {
		return ProjectContext{
			RootDir:   absDir,
			Manifest:  manifest,
			EntryPath: primaryEntry,
		}, nil
	}

	// Backwards-compatible fallback to root app.gpk.
	legacyEntry := filepath.Join(absDir, "app.gpk")
	if _, err := os.Stat(legacyEntry); err == nil {
		return ProjectContext{
			RootDir:   absDir,
			Manifest:  manifest,
			EntryPath: legacyEntry,
		}, nil
	}

	return ProjectContext{}, fmt.Errorf("entry file not found: checked %q and legacy %q", manifest.Entry, "app.gpk")
}

// ScaffoldProject creates a clean multi-platform Gopkin project at targetDir:
//   - <AppName>/gopkin.yaml
//   - <AppName>/app/main.gpk
func ScaffoldProject(baseDir, appName string) (string, error) {
	name := strings.TrimSpace(appName)
	if name == "" {
		return "", errors.New("app name is required (usage: gopkin new <AppName>)")
	}

	projectDir := filepath.Join(baseDir, name)
	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("create project directory: %w", err)
	}

	manifest := config.DefaultManifest(name)
	manifestPath := filepath.Join(projectDir, "gopkin.yaml")
	if err := os.WriteFile(manifestPath, []byte(config.MarshalManifest(manifest)), 0o644); err != nil {
		return "", fmt.Errorf("write gopkin.yaml: %w", err)
	}

	entryPath := filepath.Join(appDir, "main.gpk")
	if err := os.WriteFile(entryPath, []byte(starterMainGPK(name)), 0o644); err != nil {
		return "", fmt.Errorf("write app/main.gpk: %w", err)
	}

	return projectDir, nil
}

func starterMainGPK(appName string) string {
	return fmt.Sprintf(`package main

import . "gopkin"

func main() {
	Run(%q,
		Center(
			Card(
				Heading(%q),
				Text("Ready to build with Gopkin.").Muted(),
			),
		),
	)
}
`, appName, appName)
}

// BuildResult describes artifacts produced by BuildProject.
type BuildResult struct {
	Target             TargetSpec
	OutputDir          string
	GeneratedManifests []string
}

// BuildProject prepares build/<target>/ for projectDir, automatically generating
// native AndroidManifest.xml (for android) or Info.plist (for ios) from gopkin.yaml.
func BuildProject(projectDir string, target TargetSpec) (BuildResult, error) {
	ctx, err := ResolveProject(projectDir)
	if err != nil {
		return BuildResult{}, err
	}

	outDir := filepath.Join(ctx.RootDir, "build", target.Name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("create build output dir %q: %w", outDir, err)
	}

	res := BuildResult{
		Target:    target,
		OutputDir: outDir,
	}

	switch target.Name {
	case "android":
		xmlContent := config.GenerateAndroidManifest(ctx.Manifest)
		dest := filepath.Join(outDir, "AndroidManifest.xml")
		if err := os.WriteFile(dest, []byte(xmlContent), 0o644); err != nil {
			return res, fmt.Errorf("write AndroidManifest.xml: %w", err)
		}
		res.GeneratedManifests = append(res.GeneratedManifests, dest)

	case "ios":
		plistContent := config.GenerateIOSInfoPlist(ctx.Manifest)
		dest := filepath.Join(outDir, "Info.plist")
		if err := os.WriteFile(dest, []byte(plistContent), 0o644); err != nil {
			return res, fmt.Errorf("write Info.plist: %w", err)
		}
		res.GeneratedManifests = append(res.GeneratedManifests, dest)
	}

	return res, nil
}

// RunCLI executes the Gopkin CLI with args (excluding argv[0]) and returns an exit code.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 1
	}

	switch args[0] {
	case "new":
		return cmdNew(args[1:], stdout, stderr)
	case "build":
		return cmdBuild(args[1:], stdout, stderr)
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "gopkin: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 1
	}
}

func cmdNew(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var baseDir string
	fs.StringVar(&baseDir, "dir", ".", "parent directory for the new app")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "usage: gopkin new <AppName>")
		return 1
	}
	appName := fs.Arg(0)
	projectDir, err := ScaffoldProject(baseDir, appName)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin new: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Created Gopkin app %q at %s\n", appName, projectDir)
	fmt.Fprintf(stdout, "  - %s\n", filepath.Join(projectDir, "gopkin.yaml"))
	fmt.Fprintf(stdout, "  - %s\n", filepath.Join(projectDir, "app", "main.gpk"))
	return 0
}

func parseTargetFlags(cmdName string, args []string, stderr io.Writer) (TargetSpec, string, error) {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var targetFlag string
	fs.StringVar(&targetFlag, "target", "desktop", "target platform (desktop, windows, darwin, linux, web, android, ios, iot)")
	fs.StringVar(&targetFlag, "t", "desktop", "shorthand for --target")

	if err := fs.Parse(args); err != nil {
		return TargetSpec{}, "", err
	}

	spec, err := ParseTarget(targetFlag)
	if err != nil {
		return TargetSpec{}, "", err
	}

	projectDir := "."
	if fs.NArg() > 0 {
		projectDir = fs.Arg(0)
	}
	return spec, projectDir, nil
}

func cmdBuild(args []string, stdout, stderr io.Writer) int {
	spec, projectDir, err := parseTargetFlags("build", args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin build: %v\n", err)
		return 1
	}

	res, err := BuildProject(projectDir, spec)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin build: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Built target %s (%s/%s) -> %s\n", res.Target.Name, res.Target.GOOS, res.Target.GOARCH, res.OutputDir)
	for _, gen := range res.GeneratedManifests {
		fmt.Fprintf(stdout, "  generated %s\n", gen)
	}
	return 0
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	spec, projectDir, err := parseTargetFlags("run", args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin run: %v\n", err)
		return 1
	}

	ctx, err := ResolveProject(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin run: %v\n", err)
		return 1
	}

	if spec.Name == "android" || spec.Name == "ios" {
		if _, err := BuildProject(projectDir, spec); err != nil {
			fmt.Fprintf(stderr, "gopkin run: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "Running %s (%s) on target %s (%s/%s) [entry: %s]\n",
		ctx.Manifest.Name, ctx.Manifest.ID, spec.Name, spec.GOOS, spec.GOARCH, ctx.EntryPath)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Gopkin CLI — Pure-Go Cross-Platform Framework")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gopkin new <AppName>                          Scaffold a new app (gopkin.yaml + app/main.gpk)")
	fmt.Fprintln(w, "  gopkin run [-t|--target <target>] [dir]       Run a Gopkin app for the selected target")
	fmt.Fprintln(w, "  gopkin build [-t|--target <target>] [dir]     Build a Gopkin app and generate native manifests")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Targets: desktop (default), windows, darwin, linux, web, android, ios, iot")
}

func main() {
	os.Exit(RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
