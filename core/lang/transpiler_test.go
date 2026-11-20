package lang_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkin/core/lang"
)

func TestTranspileIndexQuickstartSnippet(t *testing.T) {
	src := `app "MyApp" {
    db.Local("app_data.db")

    Page("Devices") {
        Table("devices", columns: ["name", "status"], search: true)
    }
}`

	goSrc, err := lang.Transpile(src)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	wantSnippets := []string{
		`db.Connect(db.LocalSQL("app_data.db"))`,
		`Heading("Devices")`,
		`db.Table("devices").OrderBy("id", "DESC").Get()`,
		`DataTable([]TableColumn{Col("name", "Name"), Col("status", "Status")}, rows).Searchable(true)`,
		`Run("MyApp"`,
	}
	for _, snip := range wantSnippets {
		if !strings.Contains(goSrc, snip) {
			t.Errorf("transpiled Go missing %q\n--- got ---\n%s", snip, goSrc)
		}
	}

	assertCompilesAgainstGopkin(t, goSrc)
}

func TestTranspileDocsFormSnippet(t *testing.T) {
	src := `app "Devices" {
    db.Local("app_data.db")

    Card("New Device") {
        Form(table: "devices") {
            Input("name", label: "Device Name", required: true)
            Select("status", options: ["on", "off"])
            Toggle("auto", label: "Auto Schedule", default: true)
            Submit("Save Device")
        }
    }
}`

	goSrc, err := lang.Transpile(src)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	wantSnippets := []string{
		`db.Connect(db.LocalSQL("app_data.db"))`,
		`Card(Heading("New Device")`,
		`TextField("name", "Device Name").Required()`,
		`SelectField("status", "Status", "on", "off")`,
		`ToggleField("auto", "Auto Schedule", true)`,
		`.SubmitLabel("Save Device")`,
		`db.Table("devices").Insert(data)`,
	}
	for _, snip := range wantSnippets {
		if !strings.Contains(goSrc, snip) {
			t.Errorf("transpiled Go missing %q\n--- got ---\n%s", snip, goSrc)
		}
	}

	assertCompilesAgainstGopkin(t, goSrc)
}

func TestTranspileDashboardAuthAndMySQL(t *testing.T) {
	src := `app "AdminPanel" {
    db.MySQL("root:pass@tcp(127.0.0.1:3306)/myapp")

    Page("Devices") {
        Badge("Online", variant: "success")
        Toast("Synced", variant: "info")
        Table("devices", columns: ["id", "name", "status"], search: true, pageSize: 10) {
            Action("Delete") { row -> db.Delete("devices", id: row.id) }
        }
    }

    Page("Login") {
        AuthCard("Sign In") { email, password ->
            auth.Login(email, password)
        }
    }
}`

	goSrc, err := lang.Transpile(src)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}
	if !strings.Contains(goSrc, `db.MySQL("root:pass@tcp(127.0.0.1:3306)/myapp")`) ||
		!strings.Contains(goSrc, `Badge("Online", "success")`) ||
		!strings.Contains(goSrc, `.PageSize(10)`) ||
		!strings.Contains(goSrc, `AuthCard("Sign In"`) {
		t.Fatalf("unexpected transpiled output:\n%s", goSrc)
	}

	assertCompilesAgainstGopkin(t, goSrc)
}

func assertCompilesAgainstGopkin(t *testing.T, goSrc string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	tmpDir := t.TempDir()
	goMod := "module transpiled_check\n\ngo 1.26\n\nrequire gopkin v0.0.0\n\nreplace gopkin => " + filepath.ToSlash(repoRoot) + "\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cmd := exec.Command("go", "build", "-o", filepath.Join(tmpDir, "app.bin"), ".")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("transpiled Go failed to compile: %v\n%s\n--- source ---\n%s", err, string(out), goSrc)
	}
}
