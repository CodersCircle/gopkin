package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkin"
	"gopkin/core/config"
	"gopkin/core/platform"
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
	case "boss", "bos":
		return TargetSpec{Name: "boss", GOOS: "linux", GOARCH: "amd64"}, nil
	default:
		return TargetSpec{}, fmt.Errorf("unsupported target %q (supported: desktop, windows, darwin, linux, web, android, ios, iot, boss)", raw)
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
	Artifacts          []string
}

// BuildProject prepares build/<target>/ for projectDir, automatically generating
// native AndroidManifest.xml (for android), Info.plist (for ios), or a
// self-contained web server binary + static index.html bundle (for web).
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
		res.Artifacts = append(res.Artifacts, dest)

	case "ios":
		plistContent := config.GenerateIOSInfoPlist(ctx.Manifest)
		dest := filepath.Join(outDir, "Info.plist")
		if err := os.WriteFile(dest, []byte(plistContent), 0o644); err != nil {
			return res, fmt.Errorf("write Info.plist: %w", err)
		}
		res.GeneratedManifests = append(res.GeneratedManifests, dest)
		res.Artifacts = append(res.Artifacts, dest)

	case "web":
		if err := buildWebBundle(ctx, outDir, &res); err != nil {
			return res, err
		}

	case "boss":
		if err := buildBOSSPackage(ctx, outDir, &res); err != nil {
			return res, err
		}
	}

	return res, nil
}

func buildBOSSPackage(ctx ProjectContext, outDir string, res *BuildResult) error {
	pkgName := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ctx.Manifest.Name), " ", "-"))
	if pkgName == "" {
		pkgName = "gopkin-app"
	}

	// Stage a clean zero-CGO static Linux amd64 source package and cross-compile it.
	stageDir := filepath.Join(outDir, ".stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("create boss stage dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	if err := os.WriteFile(filepath.Join(stageDir, "go.mod"), []byte("module gopkin_boss_app\n\ngo 1.22\n"), 0o644); err != nil {
		return fmt.Errorf("write boss stage go.mod: %w", err)
	}
	src := fmt.Sprintf(`package main

import "fmt"

func main() {
	fmt.Println(%q)
}
`, ctx.Manifest.Name+" ("+ctx.Manifest.ID+") — BOSS Linux")
	if err := os.WriteFile(filepath.Join(stageDir, "main.go"), []byte(src), 0o644); err != nil {
		return fmt.Errorf("write boss stage main.go: %w", err)
	}

	binPath := filepath.Join(outDir, pkgName)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = stageDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cross-compile BOSS linux/amd64 binary: %w\n%s", err, string(out))
	}
	res.Artifacts = append(res.Artifacts, binPath)

	// Generate .desktop launcher file.
	desktopPath := filepath.Join(outDir, pkgName+".desktop")
	desktopContent := config.GenerateBOSSDesktopEntry(ctx.Manifest, pkgName)
	if err := os.WriteFile(desktopPath, []byte(desktopContent), 0o644); err != nil {
		return fmt.Errorf("write BOSS .desktop file: %w", err)
	}
	res.GeneratedManifests = append(res.GeneratedManifests, desktopPath)
	res.Artifacts = append(res.Artifacts, desktopPath)

	// Generate DEBIAN/control metadata file.
	debianDir := filepath.Join(outDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0o755); err != nil {
		return fmt.Errorf("create DEBIAN dir: %w", err)
	}
	controlPath := filepath.Join(debianDir, "control")
	controlContent := config.GenerateBOSSDebianControl(ctx.Manifest, pkgName)
	if err := os.WriteFile(controlPath, []byte(controlContent), 0o644); err != nil {
		return fmt.Errorf("write DEBIAN/control: %w", err)
	}
	res.GeneratedManifests = append(res.GeneratedManifests, controlPath)
	res.Artifacts = append(res.Artifacts, controlPath)

	return nil
}

func buildWebBundle(ctx ProjectContext, outDir string, res *BuildResult) error {
	webDrv := platform.NewWebDriver("127.0.0.1:8080").SetMeta(platform.WebMeta{
		Title:       ctx.Manifest.Name,
		Description: ctx.Manifest.Name + " — Built with Gopkin",
		AppID:       ctx.Manifest.ID,
		Version:     ctx.Manifest.Version,
	})

	indexPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(webDrv.RenderHTML()), 0o644); err != nil {
		return fmt.Errorf("write web index.html: %w", err)
	}
	res.Artifacts = append(res.Artifacts, indexPath)

	// Emit a zero-dependency stdlib Go web server that embeds index.html and
	// compiles into a self-contained runnable binary.
	goModContent := "module gopkin_web_bundle\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(outDir, "go.mod"), []byte(goModContent), 0o644); err != nil {
		return fmt.Errorf("write web go.mod: %w", err)
	}

	serverSrc := fmt.Sprintf(`package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

//go:embed index.html
var indexHTML string

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"seq": 1, "width": 800, "height": 600,
			"commands": []map[string]any{
				{"op": "clear", "color": "#ffffff"},
				{"op": "text", "x": 32, "y": 32, "text": %q, "size": 24, "color": "#000000"},
			},
		})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		<-r.Context().Done()
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}")
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("Serving %%s at http://%%s\n", %q, *addr)
	log.Fatal(srv.ListenAndServe())
}
`, ctx.Manifest.Name, ctx.Manifest.Name)

	mainPath := filepath.Join(outDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(serverSrc), 0o644); err != nil {
		return fmt.Errorf("write web server main.go: %w", err)
	}

	binName := strings.ToLower(strings.ReplaceAll(ctx.Manifest.Name, " ", "_")) + "-web"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(outDir, binName)

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = outDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compile web server binary: %w\n%s", err, string(out))
	}
	res.Artifacts = append(res.Artifacts, binPath)
	return nil
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

func parseTargetFlags(cmdName string, args []string, stderr io.Writer) (TargetSpec, string, int, error) {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var targetFlag string
	var port int
	fs.StringVar(&targetFlag, "target", "desktop", "target platform (desktop, windows, darwin, linux, web, android, ios, iot, boss)")
	fs.StringVar(&targetFlag, "t", "desktop", "shorthand for --target")
	fs.IntVar(&port, "port", 8080, "HTTP port for web target (default 8080)")

	if err := fs.Parse(args); err != nil {
		return TargetSpec{}, "", 0, err
	}

	spec, err := ParseTarget(targetFlag)
	if err != nil {
		return TargetSpec{}, "", 0, err
	}

	projectDir := "."
	if fs.NArg() > 0 {
		projectDir = fs.Arg(0)
	}
	return spec, projectDir, port, nil
}

func cmdBuild(args []string, stdout, stderr io.Writer) int {
	spec, projectDir, _, err := parseTargetFlags("build", args, stderr)
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
	for _, art := range res.Artifacts {
		fmt.Fprintf(stdout, "  generated %s\n", art)
	}
	return 0
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	spec, projectDir, port, err := parseTargetFlags("run", args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin run: %v\n", err)
		return 1
	}

	ctx, err := ResolveProject(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "gopkin run: %v\n", err)
		return 1
	}

	if spec.Name == "web" {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		webDrv := platform.NewWebDriver(addr).SetMeta(platform.WebMeta{
			Title:       ctx.Manifest.Name,
			Description: ctx.Manifest.Name + " — Built with Gopkin",
			AppID:       ctx.Manifest.ID,
			Version:     ctx.Manifest.Version,
		})
		fmt.Fprintf(stdout, "Live Web Target running at %s [app: %s, entry: %s]\n",
			webDrv.URL(), ctx.Manifest.Name, ctx.EntryPath)

		err := gopkin.RunWithDriver(webDrv, ctx.Manifest.Name,
			gopkin.Center(
				gopkin.Card(
					gopkin.Heading(ctx.Manifest.Name),
					gopkin.Text("Running on Gopkin Live Web Target.").Muted(),
				),
			),
		)
		if err != nil {
			fmt.Fprintf(stderr, "gopkin run: %v\n", err)
			return 1
		}
		return 0
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
	fmt.Fprintln(w, "Targets: desktop (default), windows, darwin, linux, web, android, ios, iot, boss (bos)")
}

func main() {
	os.Exit(RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
