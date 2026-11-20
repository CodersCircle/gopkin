package ui_test

import (
	"errors"
	"strings"
	"testing"

	"gopkin/backend/db"
	"gopkin/core/platform"
	"gopkin/core/state"
	"gopkin/core/ui"
)

func TestTextInputFocusTypingAndMasking(t *testing.T) {
	ui.ClearFocus()

	sig := state.NewSignal("")
	var changed []string
	var submitted string

	first := ui.TextInput("Username").
		Bind(sig).
		OnChange(func(v string) { changed = append(changed, v) }).
		OnSubmit(func(v string) { submitted = v })

	second := ui.TextInput("Password").Password(true)

	root := ui.Column(first, second)
	root.Layout(ui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Click inside first input to focus.
	b1 := first.Bounds()
	if !root.HandleEvent(platform.Event{Type: platform.EventClick, X: b1.X + 10, Y: b1.Y + 10}) {
		t.Fatalf("expected click on first input to be consumed")
	}
	if !first.IsFocused() || second.IsFocused() {
		t.Fatalf("expected only first input focused, got first=%v second=%v", first.IsFocused(), second.IsFocused())
	}

	// Type "Gopkni" -> Left arrow -> Backspace -> Right -> type 'i' -> wait: test mid-string editing cleanly.
	for _, r := range "Gopkni" {
		root.HandleEvent(platform.Event{Type: platform.EventChar, Rune: r})
	}
	if first.Value() != "Gopkni" || sig.Get() != "Gopkni" {
		t.Fatalf("expected 'Gopkni', got val=%q sig=%q", first.Value(), sig.Get())
	}

	// Backspace twice and type "in" -> "Gopkin".
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyBackspace})
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyBackspace})
	for _, r := range "in" {
		root.HandleEvent(platform.Event{Type: platform.EventChar, Rune: r})
	}
	if first.Value() != "Gopkin" {
		t.Fatalf("expected 'Gopkin', got %q", first.Value())
	}

	// Left arrow moves caret back by 1, inserting '!' before 'n' -> "Gopki!n", then Backspace removes '!'.
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyLeft})
	root.HandleEvent(platform.Event{Type: platform.EventChar, Rune: '!'})
	if first.Value() != "Gopki!n" {
		t.Fatalf("expected 'Gopki!n', got %q", first.Value())
	}
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyBackspace})
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyRight})
	if first.Value() != "Gopkin" {
		t.Fatalf("expected 'Gopkin' after caret edit, got %q", first.Value())
	}

	// Enter triggers OnSubmit.
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyEnter})
	if submitted != "Gopkin" {
		t.Fatalf("expected OnSubmit('Gopkin'), got %q", submitted)
	}
	if len(changed) == 0 {
		t.Fatalf("expected OnChange to be invoked during typing")
	}

	// Tab cycles focus to second (password) input.
	if !root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyTab}) {
		t.Fatalf("expected Tab to be consumed")
	}
	if first.IsFocused() || !second.IsFocused() {
		t.Fatalf("expected focus on second input after Tab, got first=%v second=%v", first.IsFocused(), second.IsFocused())
	}

	// Type into password field and verify bullet masking + caret + focus ring rendering.
	for _, r := range "s3cr3t" {
		root.HandleEvent(platform.Event{Type: platform.EventChar, Rune: r})
	}
	if second.Value() != "s3cr3t" {
		t.Fatalf("expected password value 's3cr3t', got %q", second.Value())
	}
	if second.DisplayText() != "••••••" {
		t.Fatalf("expected masked display text '••••••', got %q", second.DisplayText())
	}

	second.Error("Weak password")
	root.Layout(ui.Rect{X: 0, Y: 0, W: 400, H: 300})
	drv := platform.NewMockDriver()
	root.Render(drv)

	foundMasked := false
	foundErr := false
	for _, tc := range drv.Texts {
		if tc.Text == "••••••" {
			foundMasked = true
		}
		if tc.Text == "Weak password" {
			foundErr = true
		}
	}
	if !foundMasked || !foundErr {
		t.Fatalf("expected masked text and error message in render calls, foundMasked=%v foundErr=%v", foundMasked, foundErr)
	}

	// Escape blurs active input.
	root.HandleEvent(platform.Event{Type: platform.EventKeyDown, Key: platform.KeyEscape})
	if second.IsFocused() {
		t.Fatalf("expected Escape to blur input")
	}
}

func TestFormValidationAndSubmission(t *testing.T) {
	ui.ClearFocus()

	var captured map[string]string
	failNext := false

	form := ui.Form(
		ui.TextField("name", "Device Name").Placeholder("Light 1").Required(),
		ui.PasswordField("secret", "API Secret").Required().MinLength(6),
		ui.SelectField("zone", "Zone", "Living", "Kitchen", "Garage"),
		ui.ToggleField("enabled", "Enabled", false),
	).OnSubmit(func(values map[string]string) error {
		if failNext {
			return errors.New("server rejected request")
		}
		captured = values
		return nil
	})

	form.Layout(ui.Rect{X: 0, Y: 0, W: 420, H: 600})

	// Submitting empty form must fail validation with inline field errors.
	if err := form.Submit(); err == nil {
		t.Fatalf("expected validation error on empty form")
	}
	if form.Field("name").ErrorMessage() == "" || form.Field("secret").ErrorMessage() == "" {
		t.Fatalf("expected field error messages, got name=%q secret=%q",
			form.Field("name").ErrorMessage(), form.Field("secret").ErrorMessage())
	}
	if form.Feedback().Variant() != "error" || !form.Feedback().Visible() {
		t.Fatalf("expected visible error toast, got variant=%q visible=%v",
			form.Feedback().Variant(), form.Feedback().Visible())
	}

	// Populate valid values and test Select + Toggle defaults/changes.
	form.Field("name").SetValue("Living Room Light")
	form.Field("secret").SetValue("123456")
	form.Field("zone").SetValue("Kitchen")
	form.Field("enabled").SetValue("true")

	// Backend failure path sets inline error feedback.
	failNext = true
	if err := form.Submit(); err == nil {
		t.Fatalf("expected backend error")
	}
	if form.Feedback().Variant() != "error" || !strings.Contains(form.Feedback().Message(), "server rejected") {
		t.Fatalf("unexpected error toast: %q (%s)", form.Feedback().Message(), form.Feedback().Variant())
	}

	// Success path clears errors and displays success toast.
	failNext = false
	if err := form.Submit(); err != nil {
		t.Fatalf("expected successful submission, got %v", err)
	}
	if form.Feedback().Variant() != "success" || !form.Feedback().Visible() {
		t.Fatalf("expected success toast, got variant=%q visible=%v",
			form.Feedback().Variant(), form.Feedback().Visible())
	}
	if captured["name"] != "Living Room Light" || captured["zone"] != "Kitchen" || captured["enabled"] != "true" {
		t.Fatalf("unexpected submitted values: %#v", captured)
	}
}

func TestDataTableWithLocalSQLSearchSortPaginationAndActions(t *testing.T) {
	ui.ClearFocus()

	eng := db.LocalSQL(":memory:")
	if err := db.Connect(eng); err != nil {
		t.Fatalf("db.Connect failed: %v", err)
	}
	defer db.Close()

	seed := []map[string]any{
		{"name": "Zebra Light", "status": "on", "watts": 15},
		{"name": "Alpha Fan", "status": "off", "watts": 55},
		{"name": "Beta Light", "status": "on", "watts": 10},
		{"name": "Gamma Heater", "status": "off", "watts": 1200},
		{"name": "Delta Light", "status": "on", "watts": 20},
	}
	for _, row := range seed {
		if _, err := db.Table("devices").Insert(row); err != nil {
			t.Fatalf("seed insert failed: %v", err)
		}
	}

	rows, err := db.Table("devices").OrderBy("id", "ASC").Get()
	if err != nil {
		t.Fatalf("db.Table.Get failed: %v", err)
	}

	var deletedRow map[string]any
	table := ui.DataTable(
		[]ui.TableColumn{
			ui.Col("id", "ID", 60),
			ui.Col("name", "Name"),
			ui.Col("status", "Status", 90),
			ui.Col("watts", "Watts", 90),
		},
		rows,
	).PageSize(2).Actions(
		ui.TableAction("Delete", func(r map[string]any) {
			deletedRow = r
		}),
	)

	table.Layout(ui.Rect{X: 0, Y: 0, W: 640, H: 400})

	// Pagination: 5 rows with PageSize 2 -> 3 pages.
	if table.TotalPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", table.TotalPages())
	}
	if len(table.VisibleRows()) != 2 {
		t.Fatalf("expected 2 visible rows on page 1, got %d", len(table.VisibleRows()))
	}

	table.NextPage()
	if table.Page() != 2 || len(table.VisibleRows()) != 2 {
		t.Fatalf("expected page 2 with 2 rows, got page=%d rows=%d", table.Page(), len(table.VisibleRows()))
	}
	table.NextPage()
	if table.Page() != 3 || len(table.VisibleRows()) != 1 {
		t.Fatalf("expected page 3 with 1 row, got page=%d rows=%d", table.Page(), len(table.VisibleRows()))
	}
	table.PrevPage()
	if table.Page() != 2 {
		t.Fatalf("expected page 2 after PrevPage, got %d", table.Page())
	}

	// Click column header "Name" to sort ASC, then click again to sort DESC.
	// Header sits below search bar (40px) + gap (8px) = Y: 48..84.
	// Column 1 ("name") starts after ID (60px) -> X: 80.
	table.Layout(ui.Rect{X: 0, Y: 0, W: 640, H: 400})
	if !table.HandleEvent(platform.Event{Type: platform.EventClick, X: 80, Y: 60}) {
		t.Fatalf("expected click on Name header to be consumed")
	}
	if table.SortColumn() != "name" || table.SortDir() != "ASC" {
		t.Fatalf("expected sort name ASC, got %s %s", table.SortColumn(), table.SortDir())
	}
	vis := table.VisibleRows()
	if vis[0]["name"] != "Alpha Fan" {
		t.Fatalf("expected first sorted row 'Alpha Fan', got %v", vis[0]["name"])
	}

	// Second click toggles to DESC.
	table.HandleEvent(platform.Event{Type: platform.EventClick, X: 80, Y: 60})
	if table.SortDir() != "DESC" || table.VisibleRows()[0]["name"] != "Zebra Light" {
		t.Fatalf("expected DESC sort starting with 'Zebra Light', got dir=%s first=%v",
			table.SortDir(), table.VisibleRows()[0]["name"])
	}

	// Live search filters rows ("light" -> 3 matching rows: Zebra, Delta, Beta).
	table.SetSearch("light")
	filtered := table.FilteredRows()
	if len(filtered) != 3 {
		t.Fatalf("expected 3 filtered rows for 'light', got %d", len(filtered))
	}

	// Click Delete action button on first visible row (Zebra Light).
	table.Layout(ui.Rect{X: 0, Y: 0, W: 640, H: 400})
	// Row 0 is at Y: 48 + 36 = 84..120; single action button is near right edge (X: 640 - 76 = 564..632).
	if !table.HandleEvent(platform.Event{Type: platform.EventClick, X: 590, Y: 100}) {
		t.Fatalf("expected click on row action button to be consumed")
	}
	if deletedRow == nil || deletedRow["name"] != "Zebra Light" {
		t.Fatalf("expected Delete action to receive 'Zebra Light', got %#v", deletedRow)
	}

	// Render check with MockDriver.
	drv := platform.NewMockDriver()
	table.Render(drv)
	if len(drv.Rects) == 0 || len(drv.Texts) == 0 {
		t.Fatalf("expected DataTable to emit draw calls")
	}
}

func TestBadgeToastAndAuthCard(t *testing.T) {
	ui.ClearFocus()

	// Badge & Toast basics.
	badge := ui.Badge("Online", "success")
	badge.Layout(ui.Rect{X: 0, Y: 0, W: 200, H: 40})
	toast := ui.Toast("Device updated", "info")
	toast.Layout(ui.Rect{X: 0, Y: 0, W: 300, H: 40})

	drv := platform.NewMockDriver()
	badge.Render(drv)
	toast.Render(drv)
	if len(drv.Texts) < 2 {
		t.Fatalf("expected badge and toast text draws, got %d", len(drv.Texts))
	}
	// Clicking visible toast dismisses it.
	tb := toast.Bounds()
	if !toast.HandleEvent(platform.Event{Type: platform.EventClick, X: tb.X + 5, Y: tb.Y + 5}) || toast.Visible() {
		t.Fatalf("expected clicking toast to dismiss it")
	}

	// AuthCard login & register callbacks.
	var loginEmail, loginPass string
	var regName, regEmail, regPass string

	card := ui.AuthCard("Welcome Back", func(email, password string) error {
		if password != "secret123" {
			return errors.New("invalid credentials")
		}
		loginEmail = email
		loginPass = password
		return nil
	}).OnRegister(func(name, email, password string) error {
		regName, regEmail, regPass = name, email, password
		return nil
	})

	card.Layout(ui.Rect{X: 0, Y: 0, W: 500, H: 600})

	// Invalid credentials return error and show error toast.
	card.EmailInput().SetValue("alice@example.com")
	card.PasswordInput().SetValue("wrongpass")
	if err := card.Submit(); err == nil {
		t.Fatalf("expected invalid credentials error")
	}
	if card.Form().Feedback().Variant() != "error" {
		t.Fatalf("expected error feedback variant, got %q", card.Form().Feedback().Variant())
	}

	// Valid credentials succeed.
	card.PasswordInput().SetValue("secret123")
	if err := card.Submit(); err != nil {
		t.Fatalf("expected login success, got %v", err)
	}
	if loginEmail != "alice@example.com" || loginPass != "secret123" {
		t.Fatalf("unexpected login callback args: %q / %q", loginEmail, loginPass)
	}

	// Switch to Register mode and submit.
	card.SetMode("register")
	card.Form().Field("name").SetValue("Alice")
	card.EmailInput().SetValue("alice@example.com")
	card.PasswordInput().SetValue("newpass99")
	if err := card.Submit(); err != nil {
		t.Fatalf("expected register success, got %v", err)
	}
	if regName != "Alice" || regEmail != "alice@example.com" || regPass != "newpass99" {
		t.Fatalf("unexpected register callback args: %q / %q / %q", regName, regEmail, regPass)
	}
}

func TestStarterScreenInteractiveCounterAndThemeToggle(t *testing.T) {
	screen := ui.StarterScreen("MyApp")
	screen.Layout(ui.Rect{X: 0, Y: 0, W: 800, H: 600})

	drv := platform.NewMockDriver()
	screen.Render(drv)

	var texts []string
	for _, tc := range drv.Texts {
		texts = append(texts, tc.Text)
	}
	joined := strings.Join(texts, " | ")
	for _, want := range []string{"GOPKIN", "Welcome to MyApp", "Interactive Counter", "Documentation", "Settings", "Dark Mode"} {
		if !strings.Contains(joined, want) {
			t.Errorf("StarterScreen render missing %q (got: %s)", want, joined)
		}
	}

	// Click "+" button (centered contentX=120, contentW=560, btnY=56+92+104=252, incBtn is middle button around X=400).
	if !screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 400, Y: 270}) || screen.Count() != 1 {
		t.Fatalf("expected click on + to increment count to 1, got %d", screen.Count())
	}
	// Click "-" button (left button around X=200).
	if !screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 200, Y: 270}) || screen.Count() != 0 {
		t.Fatalf("expected click on - to decrement count to 0, got %d", screen.Count())
	}
	// Increment twice then click "Refresh" button (right button around X=580).
	screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 400, Y: 270})
	screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 400, Y: 270})
	if screen.Count() != 2 {
		t.Fatalf("expected count 2, got %d", screen.Count())
	}
	if !screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 580, Y: 270}) || screen.Count() != 0 {
		t.Fatalf("expected click on Refresh to reset count to 0, got %d", screen.Count())
	}

	// Click Dark/Light theme toggle button (top right: X=800-112-20+10=678, Y=24).
	if !screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 700, Y: 24}) || !screen.IsDark() {
		t.Fatalf("expected click on theme toggle to enable dark mode")
	}
	if !screen.HandleEvent(platform.Event{Type: platform.EventClick, X: 700, Y: 24}) || screen.IsDark() {
		t.Fatalf("expected second click on theme toggle to return to light mode")
	}
}
