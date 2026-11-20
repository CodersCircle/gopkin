package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkin/core/platform"
)

// Semantic status colors harmonized with Gopkin's clean minimalist palette.
var (
	ColorInfo    = platform.Color{R: 0x33, G: 0x33, B: 0x33, A: 0xFF} // #333333
	ColorSuccess = platform.Color{R: 0x16, G: 0x65, B: 0x34, A: 0xFF} // #166534
	ColorError   = platform.Color{R: 0xB9, G: 0x1C, B: 0x1C, A: 0xFF} // #B91C1C
)

func variantColors(variant string) (bg, fg platform.Color) {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "success":
		return ColorSuccess, DefaultTheme.Background
	case "error":
		return ColorError, DefaultTheme.Background
	case "info":
		return DefaultTheme.Primary, DefaultTheme.Background
	default:
		return DefaultTheme.Surface, DefaultTheme.Primary
	}
}

// --- Badge ---

// BadgeWidget renders a compact status pill (e.g. "on", "active", "error").
type BadgeWidget struct {
	label   string
	variant string
	bounds  Rect
}

// Badge creates a status badge widget with an optional variant ("info", "success", "error").
func Badge(label string, variant ...string) *BadgeWidget {
	v := "info"
	if len(variant) > 0 && variant[0] != "" {
		v = variant[0]
	}
	return &BadgeWidget{label: label, variant: v}
}

func (b *BadgeWidget) Type() string    { return "badge" }
func (b *BadgeWidget) Bounds() Rect    { return b.bounds }
func (b *BadgeWidget) Label() string   { return b.label }
func (b *BadgeWidget) Variant() string { return b.variant }

func (b *BadgeWidget) Layout(bounds Rect) Rect {
	w := min(bounds.W, max(float32(56), float32(len(b.label))*8+20))
	b.bounds = Rect{X: bounds.X, Y: bounds.Y, W: w, H: 24}
	return b.bounds
}

func (b *BadgeWidget) Render(driver platform.Driver) {
	bg, fg := variantColors(b.variant)
	driver.DrawRect(b.bounds.X, b.bounds.Y, b.bounds.W, b.bounds.H, bg)
	driver.DrawText(b.bounds.X+10, b.bounds.Y+5, b.label, 12, fg)
}

func (b *BadgeWidget) HandleEvent(platform.Event) bool { return false }

// --- Toast ---

// ToastWidget displays an inline feedback banner for info, success, or error messages.
type ToastWidget struct {
	message string
	variant string
	visible bool
	bounds  Rect
}

// Toast creates a feedback banner widget with an optional variant ("info", "success", "error").
func Toast(message string, variant ...string) *ToastWidget {
	v := "info"
	if len(variant) > 0 && variant[0] != "" {
		v = variant[0]
	}
	return &ToastWidget{
		message: message,
		variant: v,
		visible: message != "",
	}
}

func (t *ToastWidget) Type() string    { return "toast" }
func (t *ToastWidget) Bounds() Rect    { return t.bounds }
func (t *ToastWidget) Message() string { return t.message }
func (t *ToastWidget) Variant() string { return t.variant }
func (t *ToastWidget) Visible() bool   { return t.visible }

// Show updates the toast message and variant and makes it visible.
func (t *ToastWidget) Show(message string, variant ...string) *ToastWidget {
	t.message = message
	if len(variant) > 0 && variant[0] != "" {
		t.variant = variant[0]
	}
	t.visible = message != ""
	return t
}

// Dismiss hides the toast banner.
func (t *ToastWidget) Dismiss() *ToastWidget {
	t.visible = false
	return t
}

func (t *ToastWidget) Layout(bounds Rect) Rect {
	if !t.visible || t.message == "" {
		t.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 0}
		return t.bounds
	}
	t.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 36}
	return t.bounds
}

func (t *ToastWidget) Render(driver platform.Driver) {
	if !t.visible || t.message == "" {
		return
	}
	bg, fg := variantColors(t.variant)
	driver.DrawRect(t.bounds.X, t.bounds.Y, t.bounds.W, t.bounds.H, bg)
	driver.DrawText(t.bounds.X+12, t.bounds.Y+10, t.message, 13, fg)
}

func (t *ToastWidget) HandleEvent(ev platform.Event) bool {
	if t.visible && ev.Type == platform.EventClick && t.bounds.Contains(ev.X, ev.Y) {
		t.Dismiss()
		return true
	}
	return false
}

// --- Declarative Form ---

// FormFieldKind identifies the input control rendered for a FormField.
type FormFieldKind string

const (
	FieldText     FormFieldKind = "text"
	FieldPassword FormFieldKind = "password"
	FieldSelect   FormFieldKind = "select"
	FieldToggle   FormFieldKind = "toggle"
)

// FormField describes a labeled, validated field inside a FormWidget.
type FormField struct {
	name      string
	label     string
	kind      FormFieldKind
	required  bool
	minLength int
	validator func(string) string

	input     *TextInputWidget
	options   []string
	optionIdx int
	toggled   bool
	errMsg    string

	bounds      Rect
	ctrlBounds  Rect
}

// TextField creates a labeled text input field for Form.
func TextField(name, label string) *FormField {
	return &FormField{
		name:  name,
		label: label,
		kind:  FieldText,
		input: TextInput(),
	}
}

// PasswordField creates a labeled masked password field for Form.
func PasswordField(name, label string) *FormField {
	return &FormField{
		name:  name,
		label: label,
		kind:  FieldPassword,
		input: TextInput().Password(true),
	}
}

// SelectField creates a labeled option selector field for Form.
func SelectField(name, label string, options ...string) *FormField {
	return &FormField{
		name:    name,
		label:   label,
		kind:    FieldSelect,
		options: append([]string(nil), options...),
	}
}

// ToggleField creates a labeled boolean toggle field for Form.
func ToggleField(name, label string, initial ...bool) *FormField {
	v := false
	if len(initial) > 0 {
		v = initial[0]
	}
	return &FormField{
		name:    name,
		label:   label,
		kind:    FieldToggle,
		toggled: v,
	}
}

func (f *FormField) Name() string            { return f.name }
func (f *FormField) Label() string           { return f.label }
func (f *FormField) Kind() FormFieldKind     { return f.kind }
func (f *FormField) Input() *TextInputWidget { return f.input }
func (f *FormField) ErrorMessage() string    { return f.errMsg }

// Placeholder sets placeholder text for text/password fields.
func (f *FormField) Placeholder(text string) *FormField {
	if f.input != nil {
		f.input.Placeholder(text)
	}
	return f
}

// Default sets the initial value for the field.
func (f *FormField) Default(val string) *FormField {
	f.SetValue(val)
	return f
}

// Required marks the field as required during Form validation.
func (f *FormField) Required() *FormField {
	f.required = true
	return f
}

// MinLength requires a minimum rune count during Form validation.
func (f *FormField) MinLength(n int) *FormField {
	f.minLength = n
	return f
}

// Validator attaches a custom validation function returning an error string or "".
func (f *FormField) Validator(fn func(string) string) *FormField {
	f.validator = fn
	return f
}

// Value returns the string representation of the field's current value.
func (f *FormField) Value() string {
	switch f.kind {
	case FieldText, FieldPassword:
		if f.input != nil {
			return f.input.Value()
		}
		return ""
	case FieldSelect:
		if len(f.options) == 0 {
			return ""
		}
		return f.options[clampInt(f.optionIdx, 0, len(f.options)-1)]
	case FieldToggle:
		if f.toggled {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// SetValue updates the field's value.
func (f *FormField) SetValue(val string) {
	switch f.kind {
	case FieldText, FieldPassword:
		if f.input != nil {
			f.input.SetValue(val)
		}
	case FieldSelect:
		for i, opt := range f.options {
			if opt == val {
				f.optionIdx = i
				return
			}
		}
	case FieldToggle:
		f.toggled = strings.EqualFold(val, "true") || val == "1" || strings.EqualFold(val, "on")
	}
}

func (f *FormField) setError(msg string) {
	f.errMsg = msg
	if f.input != nil {
		f.input.Error(msg)
	}
}

func (f *FormField) validate() bool {
	val := strings.TrimSpace(f.Value())
	if f.required && val == "" {
		f.setError(f.label + " is required")
		return false
	}
	if f.minLength > 0 && len([]rune(val)) < f.minLength {
		f.setError(fmt.Sprintf("%s must be at least %d characters", f.label, f.minLength))
		return false
	}
	if f.validator != nil {
		if msg := f.validator(val); msg != "" {
			f.setError(msg)
			return false
		}
	}
	f.setError("")
	return true
}

func (f *FormField) layout(bounds Rect) Rect {
	const labelH float32 = 20
	curY := bounds.Y + labelH

	var ctrlH float32
	switch f.kind {
	case FieldText, FieldPassword:
		used := f.input.Layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: max(0, bounds.H-labelH)})
		f.ctrlBounds = used
		ctrlH = used.H
	case FieldSelect, FieldToggle:
		f.ctrlBounds = Rect{X: bounds.X, Y: curY, W: bounds.W, H: 36}
		ctrlH = 36
		if f.errMsg != "" {
			ctrlH += 20
		}
	}

	f.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: labelH + ctrlH}
	return f.bounds
}

func (f *FormField) render(driver platform.Driver) {
	labelText := f.label
	if f.required {
		labelText += " *"
	}
	driver.DrawText(f.bounds.X, f.bounds.Y+2, labelText, 13, DefaultTheme.Primary)

	switch f.kind {
	case FieldText, FieldPassword:
		f.input.Render(driver)
	case FieldSelect:
		driver.DrawRect(f.ctrlBounds.X, f.ctrlBounds.Y, f.ctrlBounds.W, f.ctrlBounds.H, DefaultTheme.Accent)
		driver.DrawRect(f.ctrlBounds.X+1, f.ctrlBounds.Y+1, max(0, f.ctrlBounds.W-2), max(0, f.ctrlBounds.H-2), DefaultTheme.Background)
		driver.DrawText(f.ctrlBounds.X+12, f.ctrlBounds.Y+10, f.Value()+" ▾", 14, DefaultTheme.Primary)
		if f.errMsg != "" {
			driver.DrawText(f.bounds.X+2, f.ctrlBounds.Y+f.ctrlBounds.H+4, f.errMsg, 12, ColorError)
		}
	case FieldToggle:
		bg := DefaultTheme.Accent
		label := "OFF"
		if f.toggled {
			bg = DefaultTheme.Primary
			label = "ON"
		}
		driver.DrawRect(f.ctrlBounds.X, f.ctrlBounds.Y, 64, 32, bg)
		driver.DrawText(f.ctrlBounds.X+16, f.ctrlBounds.Y+8, label, 13, DefaultTheme.Background)
		if f.errMsg != "" {
			driver.DrawText(f.bounds.X+2, f.ctrlBounds.Y+f.ctrlBounds.H+4, f.errMsg, 12, ColorError)
		}
	}
}

func (f *FormField) handleEvent(ev platform.Event) bool {
	switch f.kind {
	case FieldText, FieldPassword:
		return f.input.HandleEvent(ev)
	case FieldSelect:
		if ev.Type == platform.EventClick && f.ctrlBounds.Contains(ev.X, ev.Y) {
			if len(f.options) > 0 {
				f.optionIdx = (f.optionIdx + 1) % len(f.options)
			}
			return true
		}
	case FieldToggle:
		if ev.Type == platform.EventClick && f.ctrlBounds.Contains(ev.X, ev.Y) {
			f.toggled = !f.toggled
			return true
		}
	}
	return false
}

// FormWidget is a Filament-style declarative form with built-in validation,
// submit handling, and inline error/success feedback.
type FormWidget struct {
	fields         []*FormField
	submitBtn      *ButtonWidget
	feedback       *ToastWidget
	onSubmit       func(values map[string]string) error
	successMessage string
	bounds         Rect
}

// Form creates a declarative form containing the given FormFields.
func Form(fields ...*FormField) *FormWidget {
	filtered := make([]*FormField, 0, len(fields))
	for _, f := range fields {
		if f != nil {
			filtered = append(filtered, f)
		}
	}
	fw := &FormWidget{
		fields:         filtered,
		feedback:       Toast(""),
		successMessage: "Saved successfully.",
	}
	fw.submitBtn = Btn("Submit", func() {
		_ = fw.Submit()
	}).FullWidth()

	// Pressing Enter in any text/password input submits the form.
	for _, f := range fw.fields {
		if f.input != nil {
			f.input.OnSubmit(func(string) {
				_ = fw.Submit()
			})
		}
	}
	return fw
}

func (fw *FormWidget) Type() string          { return "form" }
func (fw *FormWidget) Bounds() Rect          { return fw.bounds }
func (fw *FormWidget) Feedback() *ToastWidget { return fw.feedback }
func (fw *FormWidget) SubmitButton() *ButtonWidget { return fw.submitBtn }

// Focusables returns the ordered list of focusable inputs in this form.
func (fw *FormWidget) Focusables() []Focusable {
	var out []Focusable
	for _, f := range fw.fields {
		if f.input != nil {
			out = append(out, f.input)
		}
	}
	return out
}

// SubmitLabel customizes the submit button text.
func (fw *FormWidget) SubmitLabel(label string) *FormWidget {
	fw.submitBtn.label = label
	return fw
}

// SuccessMessage customizes the inline success feedback message shown after Submit succeeds.
func (fw *FormWidget) SuccessMessage(msg string) *FormWidget {
	fw.successMessage = msg
	return fw
}

// OnSubmit sets the handler invoked when validation succeeds.
func (fw *FormWidget) OnSubmit(fn func(values map[string]string) error) *FormWidget {
	fw.onSubmit = fn
	return fw
}

// Field returns the FormField with the given name, or nil if not found.
func (fw *FormWidget) Field(name string) *FormField {
	for _, f := range fw.fields {
		if f.name == name {
			return f
		}
	}
	return nil
}

// Values returns a map of all field names to their current string values.
func (fw *FormWidget) Values() map[string]string {
	vals := make(map[string]string, len(fw.fields))
	for _, f := range fw.fields {
		vals[f.name] = f.Value()
	}
	return vals
}

// Validate runs all field validators and updates inline error messages.
func (fw *FormWidget) Validate() bool {
	valid := true
	for _, f := range fw.fields {
		if !f.validate() {
			valid = false
		}
	}
	return valid
}

// Submit validates the form, invokes OnSubmit if valid, and updates inline feedback.
func (fw *FormWidget) Submit() error {
	if !fw.Validate() {
		fw.feedback.Show("Please fix the highlighted errors.", "error")
		return fmt.Errorf("ui: form validation failed")
	}
	if fw.onSubmit != nil {
		if err := fw.onSubmit(fw.Values()); err != nil {
			fw.feedback.Show(err.Error(), "error")
			return err
		}
	}
	if fw.successMessage != "" {
		fw.feedback.Show(fw.successMessage, "success")
	}
	return nil
}

func (fw *FormWidget) Layout(bounds Rect) Rect {
	const gap float32 = 12
	curY := bounds.Y

	for _, f := range fw.fields {
		used := f.layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: max(0, bounds.Y+bounds.H-curY)})
		curY += used.H + gap
	}

	if fw.feedback.Visible() {
		used := fw.feedback.Layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: 36})
		curY += used.H + gap
	} else {
		fw.feedback.Layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: 0})
	}

	btnRect := fw.submitBtn.Layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: 40})
	curY += btnRect.H

	fw.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: curY - bounds.Y}
	return fw.bounds
}

func (fw *FormWidget) Render(driver platform.Driver) {
	for _, f := range fw.fields {
		f.render(driver)
	}
	if fw.feedback.Visible() {
		fw.feedback.Render(driver)
	}
	fw.submitBtn.Render(driver)
}

func (fw *FormWidget) HandleEvent(ev platform.Event) bool {
	if ev.Type == platform.EventKeyDown && ev.Key == platform.KeyTab {
		return CycleFocus(fw, ev.Shift)
	}
	if fw.submitBtn.HandleEvent(ev) {
		return true
	}
	if fw.feedback.HandleEvent(ev) {
		return true
	}
	for _, f := range fw.fields {
		if f.handleEvent(ev) {
			return true
		}
	}
	return false
}

// --- Responsive DataTable ---

// TableColumn defines a single column in a DataTableWidget.
type TableColumn struct {
	Key   string
	Label string
	Width float32 // optional explicit width; 0 shares remaining width equally
}

// Col is a concise constructor for TableColumn.
func Col(key, label string, width ...float32) TableColumn {
	c := TableColumn{Key: key, Label: label}
	if len(width) > 0 {
		c.Width = width[0]
	}
	return c
}

// RowAction defines an interactive button rendered per row in a DataTableWidget.
type RowAction struct {
	Label   string
	OnClick func(row map[string]any)
}

// TableAction is a concise constructor for RowAction.
func TableAction(label string, onClick func(row map[string]any)) RowAction {
	return RowAction{Label: label, OnClick: onClick}
}

type tableActionHit struct {
	rect   Rect
	row    map[string]any
	action RowAction
}

// DataTableWidget is a Filament-style data table with live search, column sorting,
// pagination, and optional per-row actions, directly compatible with []map[string]any.
type DataTableWidget struct {
	columns     []TableColumn
	rows        []map[string]any
	actions     []RowAction
	searchable  bool
	searchInput *TextInputWidget
	sortCol     string
	sortDir     string // "ASC" or "DESC"
	page        int
	pageSize    int

	bounds       Rect
	headerRects  []Rect
	actionHits   []tableActionHit
	prevBtnRect  Rect
	nextBtnRect  Rect
}

// DataTable creates a responsive DataTableWidget from columns and rows.
func DataTable(columns []TableColumn, rows []map[string]any) *DataTableWidget {
	dt := &DataTableWidget{
		columns:    append([]TableColumn(nil), columns...),
		rows:       append([]map[string]any(nil), rows...),
		searchable: true,
		page:       1,
		pageSize:   10,
	}
	dt.searchInput = TextInput("Search...").OnChange(func(string) {
		dt.page = 1
	})
	return dt
}

func (dt *DataTableWidget) Type() string                  { return "data_table" }
func (dt *DataTableWidget) Bounds() Rect                  { return dt.bounds }
func (dt *DataTableWidget) SearchInput() *TextInputWidget { return dt.searchInput }
func (dt *DataTableWidget) SortColumn() string            { return dt.sortCol }
func (dt *DataTableWidget) SortDir() string               { return dt.sortDir }
func (dt *DataTableWidget) Page() int                     { return dt.page }

// Focusables exposes the table's search bar to Tab focus cycling.
func (dt *DataTableWidget) Focusables() []Focusable {
	if dt.searchable && dt.searchInput != nil {
		return []Focusable{dt.searchInput}
	}
	return nil
}

// SetRows replaces the table's dataset and clamps the current page.
func (dt *DataTableWidget) SetRows(rows []map[string]any) *DataTableWidget {
	dt.rows = append([]map[string]any(nil), rows...)
	dt.clampPage()
	return dt
}

// PageSize sets the maximum number of rows displayed per page.
func (dt *DataTableWidget) PageSize(n int) *DataTableWidget {
	if n > 0 {
		dt.pageSize = n
		dt.clampPage()
	}
	return dt
}

// Searchable enables or disables the built-in live search bar.
func (dt *DataTableWidget) Searchable(enabled bool) *DataTableWidget {
	dt.searchable = enabled
	return dt
}

// Actions attaches per-row action buttons (e.g., Edit, Delete).
func (dt *DataTableWidget) Actions(actions ...RowAction) *DataTableWidget {
	dt.actions = append([]RowAction(nil), actions...)
	return dt
}

// SetSearch updates the live search query and resets pagination to page 1.
func (dt *DataTableWidget) SetSearch(query string) *DataTableWidget {
	dt.searchInput.SetValue(query)
	dt.page = 1
	return dt
}

// SortBy sorts the table by columnKey, toggling ASC/DESC if already active.
func (dt *DataTableWidget) SortBy(columnKey string) *DataTableWidget {
	if dt.sortCol == columnKey {
		if dt.sortDir == "ASC" {
			dt.sortDir = "DESC"
		} else {
			dt.sortDir = "ASC"
		}
	} else {
		dt.sortCol = columnKey
		dt.sortDir = "ASC"
	}
	dt.page = 1
	return dt
}

// TotalPages returns the total number of pages for the current filtered rows.
func (dt *DataTableWidget) TotalPages() int {
	total := len(dt.FilteredRows())
	if total == 0 {
		return 1
	}
	return (total + dt.pageSize - 1) / dt.pageSize
}

// SetPage navigates to a 1-based page index, clamped to valid bounds.
func (dt *DataTableWidget) SetPage(page int) *DataTableWidget {
	dt.page = page
	dt.clampPage()
	return dt
}

// NextPage advances to the next page if available.
func (dt *DataTableWidget) NextPage() *DataTableWidget {
	return dt.SetPage(dt.page + 1)
}

// PrevPage moves to the previous page if available.
func (dt *DataTableWidget) PrevPage() *DataTableWidget {
	return dt.SetPage(dt.page - 1)
}

func (dt *DataTableWidget) clampPage() {
	maxPage := dt.TotalPages()
	if dt.page < 1 {
		dt.page = 1
	}
	if dt.page > maxPage {
		dt.page = maxPage
	}
}

// FilteredRows returns all rows matching the live search query, sorted by the active column.
func (dt *DataTableWidget) FilteredRows() []map[string]any {
	q := strings.ToLower(strings.TrimSpace(dt.searchInput.Value()))
	out := make([]map[string]any, 0, len(dt.rows))

	for _, row := range dt.rows {
		if q == "" {
			out = append(out, row)
			continue
		}
		matched := false
		for _, col := range dt.columns {
			if val, ok := row[col.Key]; ok && val != nil {
				if strings.Contains(strings.ToLower(fmt.Sprint(val)), q) {
					matched = true
					break
				}
			}
		}
		if matched {
			out = append(out, row)
		}
	}

	if dt.sortCol != "" {
		desc := dt.sortDir == "DESC"
		sort.SliceStable(out, func(i, j int) bool {
			cmp := compareTableValues(out[i][dt.sortCol], out[j][dt.sortCol])
			if desc {
				return cmp > 0
			}
			return cmp < 0
		})
	}
	return out
}

// VisibleRows returns the slice of rows displayed on the current page.
func (dt *DataTableWidget) VisibleRows() []map[string]any {
	filtered := dt.FilteredRows()
	dt.clampPage()
	if len(filtered) == 0 {
		return []map[string]any{}
	}
	start := (dt.page - 1) * dt.pageSize
	if start >= len(filtered) {
		return []map[string]any{}
	}
	end := min(start+dt.pageSize, len(filtered))
	return filtered[start:end]
}

func (dt *DataTableWidget) computeColumnWidths(totalW float32) ([]float32, float32) {
	actionW := float32(0)
	if len(dt.actions) > 0 {
		actionW = float32(len(dt.actions))*76 + 8
	}
	availW := max(0, totalW-actionW)

	widths := make([]float32, len(dt.columns))
	fixedSum := float32(0)
	autoCount := 0
	for i, col := range dt.columns {
		if col.Width > 0 {
			widths[i] = col.Width
			fixedSum += col.Width
		} else {
			autoCount++
		}
	}
	if autoCount > 0 {
		rem := max(0, availW-fixedSum)
		each := rem / float32(autoCount)
		for i, col := range dt.columns {
			if col.Width <= 0 {
				widths[i] = each
			}
		}
	}
	return widths, actionW
}

func (dt *DataTableWidget) Layout(bounds Rect) Rect {
	const headerH float32 = 36
	const rowH float32 = 36
	const footerH float32 = 36
	const gap float32 = 8

	curY := bounds.Y
	if dt.searchable {
		used := dt.searchInput.Layout(Rect{X: bounds.X, Y: curY, W: bounds.W, H: 40})
		curY += used.H + gap
	}

	colWidths, actionW := dt.computeColumnWidths(bounds.W)
	dt.headerRects = make([]Rect, len(dt.columns))
	curX := bounds.X
	for i, w := range colWidths {
		dt.headerRects[i] = Rect{X: curX, Y: curY, W: w, H: headerH}
		curX += w
	}
	curY += headerH

	visible := dt.VisibleRows()
	dt.actionHits = dt.actionHits[:0]
	for _, row := range visible {
		if len(dt.actions) > 0 {
			actX := bounds.X + bounds.W - actionW + 4
			for _, act := range dt.actions {
				r := Rect{X: actX, Y: curY + 4, W: 68, H: rowH - 8}
				dt.actionHits = append(dt.actionHits, tableActionHit{
					rect:   r,
					row:    row,
					action: act,
				})
				actX += 76
			}
		}
		curY += rowH
	}
	if len(visible) == 0 {
		curY += rowH // empty-state row
	}

	curY += gap
	dt.prevBtnRect = Rect{X: bounds.X + max(0, bounds.W-152), Y: curY, W: 72, H: 32}
	dt.nextBtnRect = Rect{X: bounds.X + max(0, bounds.W-72), Y: curY, W: 72, H: 32}
	curY += footerH

	dt.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: curY - bounds.Y}
	return dt.bounds
}

func (dt *DataTableWidget) Render(driver platform.Driver) {
	if dt.searchable {
		dt.searchInput.Render(driver)
	}

	// Header row
	if len(dt.headerRects) > 0 {
		hdrY := dt.headerRects[0].Y
		hdrH := dt.headerRects[0].H
		driver.DrawRect(dt.bounds.X, hdrY, dt.bounds.W, hdrH, DefaultTheme.Primary)
		for i, col := range dt.columns {
			label := col.Label
			if dt.sortCol == col.Key {
				if dt.sortDir == "DESC" {
					label += " ▼"
				} else {
					label += " ▲"
				}
			}
			driver.DrawText(dt.headerRects[i].X+10, hdrY+10, label, 13, DefaultTheme.Background)
		}
		if len(dt.actions) > 0 {
			_, actionW := dt.computeColumnWidths(dt.bounds.W)
			driver.DrawText(dt.bounds.X+dt.bounds.W-actionW+10, hdrY+10, "Actions", 13, DefaultTheme.Background)
		}

		// Data rows
		curY := hdrY + hdrH
		const rowH float32 = 36
		visible := dt.VisibleRows()
		if len(visible) == 0 {
			driver.DrawRect(dt.bounds.X, curY, dt.bounds.W, rowH, DefaultTheme.Surface)
			driver.DrawText(dt.bounds.X+12, curY+10, "No records found.", 13, DefaultTheme.Accent)
		} else {
			for rIdx, row := range visible {
				bg := DefaultTheme.Background
				if rIdx%2 == 1 {
					bg = DefaultTheme.Surface
				}
				driver.DrawRect(dt.bounds.X, curY, dt.bounds.W, rowH, bg)
				driver.DrawRect(dt.bounds.X, curY+rowH-1, dt.bounds.W, 1, DefaultTheme.Accent)
				for cIdx, col := range dt.columns {
					valStr := ""
					if v, ok := row[col.Key]; ok && v != nil {
						valStr = fmt.Sprint(v)
					}
					driver.DrawText(dt.headerRects[cIdx].X+10, curY+10, valStr, 13, DefaultTheme.Primary)
				}
				curY += rowH
			}
		}

		// Row action buttons
		for _, hit := range dt.actionHits {
			driver.DrawRect(hit.rect.X, hit.rect.Y, hit.rect.W, hit.rect.H, DefaultTheme.Primary)
			driver.DrawText(hit.rect.X+10, hit.rect.Y+7, hit.action.Label, 12, DefaultTheme.Background)
		}
	}

	// Footer pagination controls
	totalRows := len(dt.FilteredRows())
	summary := fmt.Sprintf("Page %d of %d (%d rows)", dt.page, dt.TotalPages(), totalRows)
	driver.DrawText(dt.bounds.X+4, dt.prevBtnRect.Y+8, summary, 13, DefaultTheme.Primary)

	driver.DrawRect(dt.prevBtnRect.X, dt.prevBtnRect.Y, dt.prevBtnRect.W, dt.prevBtnRect.H, DefaultTheme.Primary)
	driver.DrawText(dt.prevBtnRect.X+18, dt.prevBtnRect.Y+8, "Prev", 13, DefaultTheme.Background)

	driver.DrawRect(dt.nextBtnRect.X, dt.nextBtnRect.Y, dt.nextBtnRect.W, dt.nextBtnRect.H, DefaultTheme.Primary)
	driver.DrawText(dt.nextBtnRect.X+18, dt.nextBtnRect.Y+8, "Next", 13, DefaultTheme.Background)
}

func (dt *DataTableWidget) HandleEvent(ev platform.Event) bool {
	if dt.searchable && dt.searchInput.HandleEvent(ev) {
		return true
	}
	if ev.Type == platform.EventClick {
		for i, r := range dt.headerRects {
			if r.Contains(ev.X, ev.Y) {
				dt.SortBy(dt.columns[i].Key)
				return true
			}
		}
		for _, hit := range dt.actionHits {
			if hit.rect.Contains(ev.X, ev.Y) {
				if hit.action.OnClick != nil {
					hit.action.OnClick(hit.row)
				}
				return true
			}
		}
		if dt.prevBtnRect.Contains(ev.X, ev.Y) {
			dt.PrevPage()
			return true
		}
		if dt.nextBtnRect.Contains(ev.X, ev.Y) {
			dt.NextPage()
			return true
		}
	}
	return false
}

func compareTableValues(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	af, okA := parseTableFloat(a)
	bf, okB := parseTableFloat(b)
	if okA && okB {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as := strings.ToLower(fmt.Sprint(a))
	bs := strings.ToLower(fmt.Sprint(b))
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func parseTableFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// --- Plug-and-Play AuthCard ---

// AuthCardWidget is a plug-and-play Login / Register card built on Card and Form.
type AuthCardWidget struct {
	title      string
	mode       string // "login" or "register"
	onLogin    func(email, password string) error
	onRegister func(name, email, password string) error

	card      *CardWidget
	heading   *TextWidget
	form      *FormWidget
	modeBtn   *ButtonWidget
	nameField *FormField
	bounds    Rect
}

// AuthCard creates a plug-and-play authentication card backed by onLogin.
func AuthCard(title string, onLogin func(email, password string) error) *AuthCardWidget {
	ac := &AuthCardWidget{
		title:   title,
		mode:    "login",
		onLogin: onLogin,
	}
	ac.rebuild()
	return ac
}

func (ac *AuthCardWidget) Type() string     { return "auth_card" }
func (ac *AuthCardWidget) Bounds() Rect     { return ac.bounds }
func (ac *AuthCardWidget) Mode() string     { return ac.mode }
func (ac *AuthCardWidget) Form() *FormWidget { return ac.form }

// Focusables exposes the inner form's inputs for Tab focus cycling.
func (ac *AuthCardWidget) Focusables() []Focusable {
	if ac.form == nil {
		return nil
	}
	return ac.form.Focusables()
}

// EmailInput returns the underlying email TextInputWidget.
func (ac *AuthCardWidget) EmailInput() *TextInputWidget {
	if f := ac.form.Field("email"); f != nil {
		return f.Input()
	}
	return nil
}

// PasswordInput returns the underlying password TextInputWidget.
func (ac *AuthCardWidget) PasswordInput() *TextInputWidget {
	if f := ac.form.Field("password"); f != nil {
		return f.Input()
	}
	return nil
}

// OnRegister enables Register mode and sets the registration callback.
func (ac *AuthCardWidget) OnRegister(fn func(name, email, password string) error) *AuthCardWidget {
	ac.onRegister = fn
	ac.rebuild()
	return ac
}

// SetMode switches between "login" and "register" modes.
func (ac *AuthCardWidget) SetMode(mode string) *AuthCardWidget {
	if strings.EqualFold(mode, "register") {
		ac.mode = "register"
	} else {
		ac.mode = "login"
	}
	ac.rebuild()
	return ac
}

// SubmitLabel overrides the form's submit button label.
func (ac *AuthCardWidget) SubmitLabel(label string) *AuthCardWidget {
	if ac.form != nil {
		ac.form.SubmitLabel(label)
	}
	return ac
}

// Submit validates and submits the active Login or Register form.
func (ac *AuthCardWidget) Submit() error {
	return ac.form.Submit()
}

func (ac *AuthCardWidget) rebuild() {
	emailField := TextField("email", "Email").Placeholder("you@example.com").Required()
	passField := PasswordField("password", "Password").Placeholder("••••••••").Required().MinLength(6)

	var fields []*FormField
	submitLabel := "Sign In"
	successMsg := "Signed in successfully."
	headingTitle := ac.title
	if headingTitle == "" {
		headingTitle = "Sign In"
	}

	if ac.mode == "register" {
		ac.nameField = TextField("name", "Name").Placeholder("Your name").Required()
		fields = []*FormField{ac.nameField, emailField, passField}
		submitLabel = "Create Account"
		successMsg = "Account created successfully."
	} else {
		ac.nameField = nil
		fields = []*FormField{emailField, passField}
	}

	ac.heading = Heading(headingTitle)
	ac.form = Form(fields...).
		SubmitLabel(submitLabel).
		SuccessMessage(successMsg).
		OnSubmit(func(vals map[string]string) error {
			if ac.mode == "register" {
				if ac.onRegister != nil {
					return ac.onRegister(vals["name"], vals["email"], vals["password"])
				}
				return nil
			}
			if ac.onLogin != nil {
				return ac.onLogin(vals["email"], vals["password"])
			}
			return nil
		})

	children := []Widget{ac.heading, ac.form}
	if ac.onRegister != nil {
		toggleLabel := "Need an account? Register"
		nextMode := "register"
		if ac.mode == "register" {
			toggleLabel = "Already have an account? Sign In"
			nextMode = "login"
		}
		ac.modeBtn = Btn(toggleLabel, func() {
			ac.SetMode(nextMode)
		}).FullWidth()
		children = append(children, ac.modeBtn)
	} else {
		ac.modeBtn = nil
	}

	ac.card = Card(children...).Padding(20)
}

func (ac *AuthCardWidget) Layout(bounds Rect) Rect {
	w := min(bounds.W, float32(420))
	ac.bounds = ac.card.Layout(Rect{X: bounds.X, Y: bounds.Y, W: w, H: bounds.H})
	return ac.bounds
}

func (ac *AuthCardWidget) Render(driver platform.Driver) {
	ac.card.Render(driver)
}

func (ac *AuthCardWidget) HandleEvent(ev platform.Event) bool {
	if ev.Type == platform.EventKeyDown && ev.Key == platform.KeyTab {
		return CycleFocus(ac, ev.Shift)
	}
	return ac.card.HandleEvent(ev)
}
