package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const localSQLFormatVersion = 1

type localTable struct {
	Name    string            `json:"name"`
	Columns map[string]string `json:"columns"`
	NextID  int64             `json:"next_id"`
	Rows    []map[string]any  `json:"rows"`
}

type localSnapshot struct {
	Version int                    `json:"version"`
	Tables  map[string]*localTable `json:"tables"`
}

// LocalSQLEngine is a 100% Pure-Go, zero-CGO, thread-safe, ACID-safe embedded
// database stored in a single file on device storage.
type LocalSQLEngine struct {
	mu        sync.RWMutex
	filePath  string
	inMemory  bool
	connected bool
	tables    map[string]*localTable
}

// LocalSQL creates a new LocalSQLEngine backed by filePath.
// Pass ":memory:" for an ephemeral in-memory database.
func LocalSQL(filePath string) *LocalSQLEngine {
	return &LocalSQLEngine{
		filePath: filePath,
		inMemory: filePath == "" || filePath == ":memory:",
		tables:   make(map[string]*localTable),
	}
}

// Connect opens (or creates) the embedded database file and loads its state.
func (l *LocalSQLEngine) Connect() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.inMemory {
		if l.tables == nil {
			l.tables = make(map[string]*localTable)
		}
		l.connected = true
		return nil
	}

	dir := filepath.Dir(l.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("db: create directory %q: %w", dir, err)
		}
	}

	data, err := os.ReadFile(l.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.tables = make(map[string]*localTable)
			l.connected = true
			return l.persistLocked()
		}
		return fmt.Errorf("db: read local database %q: %w", l.filePath, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		l.tables = make(map[string]*localTable)
		l.connected = true
		return l.persistLocked()
	}

	snap, err := decodeSnapshot(data)
	if err != nil {
		return fmt.Errorf("db: decode local database %q: %w", l.filePath, err)
	}
	if snap.Tables == nil {
		snap.Tables = make(map[string]*localTable)
	}
	for name, tbl := range snap.Tables {
		if tbl.Columns == nil {
			tbl.Columns = make(map[string]string)
		}
		if tbl.Rows == nil {
			tbl.Rows = make([]map[string]any, 0)
		}
		tbl.Name = name
	}
	l.tables = snap.Tables
	l.connected = true
	return nil
}

// Close flushes state and marks the engine as closed.
func (l *LocalSQLEngine) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.connected {
		return nil
	}
	var err error
	if !l.inMemory {
		err = l.persistLocked()
	}
	l.connected = false
	return err
}

// AutoMigrate creates the table if missing and merges any new columns into its schema.
func (l *LocalSQLEngine) AutoMigrate(table string, columns map[string]string) error {
	if err := validateIdentifier(table); err != nil {
		return err
	}
	for col := range columns {
		if err := validateIdentifier(col); err != nil {
			return err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.connected {
		return ErrNoConnection
	}

	tbl, existed := l.tables[table]
	var prevCols map[string]string
	if existed {
		prevCols = make(map[string]string, len(tbl.Columns))
		for k, v := range tbl.Columns {
			prevCols[k] = v
		}
	} else {
		tbl = &localTable{
			Name:    table,
			Columns: map[string]string{"id": "INTEGER"},
			Rows:    make([]map[string]any, 0),
		}
		l.tables[table] = tbl
	}

	for col, colType := range columns {
		if colType == "" {
			colType = "TEXT"
		}
		tbl.Columns[col] = strings.ToUpper(colType)
	}

	if err := l.persistLocked(); err != nil {
		if !existed {
			delete(l.tables, table)
		} else {
			tbl.Columns = prevCols
		}
		return err
	}
	return nil
}

// Insert adds a row to table (auto-creating the table and columns if needed) and returns its ID.
func (l *LocalSQLEngine) Insert(table string, data map[string]any) (int64, error) {
	if err := validateIdentifier(table); err != nil {
		return 0, err
	}
	for col := range data {
		if err := validateIdentifier(col); err != nil {
			return 0, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.connected {
		return 0, ErrNoConnection
	}

	tbl, existed := l.tables[table]
	prevNextID := int64(0)
	var prevCols map[string]string
	if existed {
		prevNextID = tbl.NextID
		prevCols = make(map[string]string, len(tbl.Columns))
		for k, v := range tbl.Columns {
			prevCols[k] = v
		}
	} else {
		tbl = &localTable{
			Name:    table,
			Columns: map[string]string{"id": "INTEGER"},
			Rows:    make([]map[string]any, 0),
		}
		l.tables[table] = tbl
	}

	row := make(map[string]any, len(data)+1)
	for k, v := range data {
		norm := normalizeValue(v)
		row[k] = norm
		if _, ok := tbl.Columns[k]; !ok {
			tbl.Columns[k] = inferColumnType(norm)
		}
	}

	var id int64
	if rawID, hasID := row["id"]; hasID && rawID != nil {
		if parsed, ok := toInt64(rawID); ok && parsed > 0 {
			id = parsed
			row["id"] = id
			if id > tbl.NextID {
				tbl.NextID = id
			}
		} else {
			tbl.NextID++
			id = tbl.NextID
			row["id"] = id
		}
	} else {
		tbl.NextID++
		id = tbl.NextID
		row["id"] = id
	}

	tbl.Rows = append(tbl.Rows, row)

	if err := l.persistLocked(); err != nil {
		// Roll back in-memory mutation to preserve ACID atomicity.
		if !existed {
			delete(l.tables, table)
		} else {
			tbl.Rows = tbl.Rows[:len(tbl.Rows)-1]
			tbl.NextID = prevNextID
			tbl.Columns = prevCols
		}
		return 0, err
	}
	return id, nil
}

// Find returns all rows matching query. If the table does not exist yet, it returns an empty slice.
func (l *LocalSQLEngine) Find(query Query) ([]map[string]any, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return nil, err
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.connected {
		return nil, ErrNoConnection
	}

	tbl, ok := l.tables[query.Table]
	if !ok {
		return []map[string]any{}, nil
	}

	matched := make([]map[string]any, 0, len(tbl.Rows))
	for _, row := range tbl.Rows {
		if matchesAll(row, query.Where) {
			matched = append(matched, cloneRow(row))
		}
	}

	if query.OrderBy != "" {
		desc := strings.EqualFold(query.OrderDir, "DESC")
		sort.SliceStable(matched, func(i, j int) bool {
			cmp := compareValues(matched[i][query.OrderBy], matched[j][query.OrderBy])
			if desc {
				return cmp > 0
			}
			return cmp < 0
		})
	}

	if query.Offset > 0 {
		if query.Offset >= len(matched) {
			return []map[string]any{}, nil
		}
		matched = matched[query.Offset:]
	}

	if query.Limit > 0 && query.Limit < len(matched) {
		matched = matched[:query.Limit]
	}

	return matched, nil
}

// Update updates all rows matching query.Where with data and returns the affected row count.
func (l *LocalSQLEngine) Update(query Query, data map[string]any) (int64, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return 0, err
	}
	for col := range data {
		if err := validateIdentifier(col); err != nil {
			return 0, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.connected {
		return 0, ErrNoConnection
	}

	tbl, ok := l.tables[query.Table]
	if !ok {
		return 0, nil
	}

	type backupEntry struct {
		index int
		row   map[string]any
	}
	var backups []backupEntry
	prevCols := make(map[string]string, len(tbl.Columns))
	for k, v := range tbl.Columns {
		prevCols[k] = v
	}

	var affected int64
	for i, row := range tbl.Rows {
		if matchesAll(row, query.Where) {
			backups = append(backups, backupEntry{index: i, row: cloneRow(row)})
			for k, v := range data {
				norm := normalizeValue(v)
				row[k] = norm
				if _, exists := tbl.Columns[k]; !exists {
					tbl.Columns[k] = inferColumnType(norm)
				}
			}
			affected++
		}
	}

	if affected == 0 {
		return 0, nil
	}

	if err := l.persistLocked(); err != nil {
		for _, b := range backups {
			tbl.Rows[b.index] = b.row
		}
		tbl.Columns = prevCols
		return 0, err
	}
	return affected, nil
}

// Delete removes all rows matching query.Where and returns the number of deleted rows.
func (l *LocalSQLEngine) Delete(query Query) (int64, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return 0, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.connected {
		return 0, ErrNoConnection
	}

	tbl, ok := l.tables[query.Table]
	if !ok {
		return 0, nil
	}

	prevRows := tbl.Rows
	remaining := make([]map[string]any, 0, len(tbl.Rows))
	var deleted int64
	for _, row := range tbl.Rows {
		if matchesAll(row, query.Where) {
			deleted++
			continue
		}
		remaining = append(remaining, row)
	}

	if deleted == 0 {
		return 0, nil
	}

	tbl.Rows = remaining
	if err := l.persistLocked(); err != nil {
		tbl.Rows = prevRows
		return 0, err
	}
	return deleted, nil
}

// RawQuery executes basic SQL statements (SELECT, INSERT, UPDATE, DELETE, CREATE TABLE)
// against LocalSQL with optional ? positional arguments.
func (l *LocalSQLEngine) RawQuery(sqlStr string, args ...any) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sqlStr), ";"))
	if trimmed == "" {
		return nil, errors.New("db: empty SQL statement")
	}

	interpolated, err := interpolatePlaceholders(trimmed, args)
	if err != nil {
		return nil, err
	}

	upper := strings.ToUpper(interpolated)
	switch {
	case strings.HasPrefix(upper, "SELECT "):
		return l.execRawSelect(interpolated)
	case strings.HasPrefix(upper, "DELETE "):
		return l.execRawDelete(interpolated)
	case strings.HasPrefix(upper, "CREATE TABLE "):
		return l.execRawCreateTable(interpolated)
	default:
		return nil, fmt.Errorf("db: LocalSQL RawQuery does not support statement: %q", trimmed)
	}
}

func (l *LocalSQLEngine) execRawSelect(stmt string) ([]map[string]any, error) {
	upper := strings.ToUpper(stmt)
	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx < 0 {
		return nil, fmt.Errorf("db: invalid SELECT syntax: %q", stmt)
	}

	colsPart := strings.TrimSpace(stmt[len("SELECT "):fromIdx])
	rest := strings.TrimSpace(stmt[fromIdx+len(" FROM "):])

	q, err := parseSelectClauses(rest)
	if err != nil {
		return nil, err
	}

	rows, err := l.Find(q)
	if err != nil {
		return nil, err
	}

	if colsPart == "*" {
		return rows, nil
	}

	cols := strings.Split(colsPart, ",")
	for i := range cols {
		cols[i] = strings.Trim(strings.TrimSpace(cols[i]), "`\"")
	}
	projected := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out := make(map[string]any, len(cols))
		for _, c := range cols {
			if val, ok := r[c]; ok {
				out[c] = val
			}
		}
		projected = append(projected, out)
	}
	return projected, nil
}

func (l *LocalSQLEngine) execRawDelete(stmt string) ([]map[string]any, error) {
	upper := strings.ToUpper(stmt)
	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx < 0 {
		return nil, fmt.Errorf("db: invalid DELETE syntax: %q", stmt)
	}
	rest := strings.TrimSpace(stmt[fromIdx+len(" FROM "):])
	q, err := parseSelectClauses(rest)
	if err != nil {
		return nil, err
	}
	n, err := l.Delete(q)
	if err != nil {
		return nil, err
	}
	return []map[string]any{{"affected_rows": n}}, nil
}

func (l *LocalSQLEngine) execRawCreateTable(stmt string) ([]map[string]any, error) {
	rest := strings.TrimSpace(stmt[len("CREATE TABLE "):])
	upperRest := strings.ToUpper(rest)
	if strings.HasPrefix(upperRest, "IF NOT EXISTS ") {
		rest = strings.TrimSpace(rest[len("IF NOT EXISTS "):])
	}
	openParen := strings.Index(rest, "(")
	closeParen := strings.LastIndex(rest, ")")
	if openParen < 0 || closeParen <= openParen {
		tableName := strings.Trim(strings.TrimSpace(rest), "`\"")
		if err := l.AutoMigrate(tableName, map[string]string{}); err != nil {
			return nil, err
		}
		return []map[string]any{}, nil
	}

	tableName := strings.Trim(strings.TrimSpace(rest[:openParen]), "`\"")
	colDefs := strings.Split(rest[openParen+1:closeParen], ",")
	cols := make(map[string]string)
	for _, def := range colDefs {
		parts := strings.Fields(strings.TrimSpace(def))
		if len(parts) >= 2 {
			colName := strings.Trim(parts[0], "`\"")
			cols[colName] = strings.ToUpper(parts[1])
		}
	}
	if err := l.AutoMigrate(tableName, cols); err != nil {
		return nil, err
	}
	return []map[string]any{}, nil
}

func parseSelectClauses(rest string) (Query, error) {
	var q Query
	tokens := strings.Fields(rest)
	if len(tokens) == 0 {
		return q, errors.New("db: missing table name")
	}
	q.Table = strings.Trim(tokens[0], "`\"")
	rem := strings.TrimSpace(rest[len(tokens[0]):])
	if rem == "" {
		return q, nil
	}

	// Split clauses by WHERE, ORDER BY, LIMIT, OFFSET.
	upperRem := strings.ToUpper(rem)
	extractClause := func(keyword string, stopKeywords []string) string {
		idx := strings.Index(upperRem, keyword)
		if idx < 0 {
			return ""
		}
		start := idx + len(keyword)
		end := len(rem)
		for _, stop := range stopKeywords {
			if sIdx := strings.Index(upperRem[start:], stop); sIdx >= 0 && start+sIdx < end {
				end = start + sIdx
			}
		}
		return strings.TrimSpace(rem[start:end])
	}

	if whereStr := extractClause("WHERE ", []string{" ORDER BY ", " LIMIT ", " OFFSET "}); whereStr != "" {
		parts := splitKeyword(whereStr, " AND ")
		for _, p := range parts {
			cond, err := parseCondition(p)
			if err != nil {
				return q, err
			}
			q.Where = append(q.Where, cond)
		}
	}

	if orderStr := extractClause("ORDER BY ", []string{" LIMIT ", " OFFSET "}); orderStr != "" {
		fields := strings.Fields(orderStr)
		if len(fields) >= 1 {
			q.OrderBy = strings.Trim(fields[0], "`\"")
			q.OrderDir = "ASC"
		}
		if len(fields) >= 2 {
			q.OrderDir = strings.ToUpper(fields[1])
		}
	}

	if limitStr := extractClause("LIMIT ", []string{" OFFSET "}); limitStr != "" {
		n, err := strconv.Atoi(strings.Fields(limitStr)[0])
		if err != nil {
			return q, fmt.Errorf("db: invalid LIMIT %q", limitStr)
		}
		q.Limit = n
	}

	if offsetStr := extractClause("OFFSET ", nil); offsetStr != "" {
		n, err := strconv.Atoi(strings.Fields(offsetStr)[0])
		if err != nil {
			return q, fmt.Errorf("db: invalid OFFSET %q", offsetStr)
		}
		q.Offset = n
	}

	return q, nil
}

func splitKeyword(s, sep string) []string {
	upper := strings.ToUpper(s)
	var parts []string
	start := 0
	for {
		idx := strings.Index(upper[start:], sep)
		if idx < 0 {
			parts = append(parts, strings.TrimSpace(s[start:]))
			break
		}
		parts = append(parts, strings.TrimSpace(s[start:start+idx]))
		start += idx + len(sep)
	}
	return parts
}

func parseCondition(expr string) (Condition, error) {
	ops := []string{"!=", "<>", ">=", "<=", "=", ">", "<", " LIKE "}
	upper := strings.ToUpper(expr)
	for _, op := range ops {
		idx := strings.Index(upper, op)
		if idx > 0 {
			col := strings.Trim(strings.TrimSpace(expr[:idx]), "`\"")
			rawVal := strings.TrimSpace(expr[idx+len(op):])
			return Condition{
				Column: col,
				Op:     strings.TrimSpace(op),
				Value:  parseSQLLiteral(rawVal),
			}, nil
		}
	}
	return Condition{}, fmt.Errorf("db: invalid WHERE condition %q", expr)
}

func parseSQLLiteral(s string) any {
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		inner := s[1 : len(s)-1]
		return strings.ReplaceAll(inner, "''", "'")
	}
	if strings.EqualFold(s, "NULL") {
		return nil
	}
	if strings.EqualFold(s, "TRUE") {
		return true
	}
	if strings.EqualFold(s, "FALSE") {
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func interpolatePlaceholders(sqlStr string, args []any) (string, error) {
	if len(args) == 0 {
		return sqlStr, nil
	}
	var b strings.Builder
	argIdx := 0
	inSingle := false
	for i := 0; i < len(sqlStr); i++ {
		ch := sqlStr[i]
		if ch == '\'' {
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inSingle {
			if argIdx >= len(args) {
				return "", fmt.Errorf("db: not enough arguments for SQL placeholders")
			}
			b.WriteString(formatSQLLiteral(args[argIdx]))
			argIdx++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String(), nil
}

// persistLocked atomically writes the entire database snapshot using write-temp + fsync + rename.
func (l *LocalSQLEngine) persistLocked() error {
	if l.inMemory {
		return nil
	}

	snap := localSnapshot{
		Version: localSQLFormatVersion,
		Tables:  l.tables,
	}

	payload, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("db: marshal local database: %w", err)
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(l.filePath)
	if dir == "" {
		dir = "."
	}
	tmpFile, err := os.CreateTemp(dir, ".gopkin-db-*.tmp")
	if err != nil {
		return fmt.Errorf("db: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmpFile.Write(payload); err != nil {
		cleanup()
		return fmt.Errorf("db: write temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("db: sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("db: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, l.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("db: atomic rename to %q: %w", l.filePath, err)
	}
	return nil
}

func decodeSnapshot(data []byte) (*localSnapshot, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var snap localSnapshot
	if err := dec.Decode(&snap); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	for _, tbl := range snap.Tables {
		for i, row := range tbl.Rows {
			tbl.Rows[i] = normalizeRow(row)
		}
	}
	return &snap, nil
}

func normalizeRow(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i
		}
		if f, err := val.Float64(); err == nil {
			return f
		}
		return val.String()
	case int:
		return int64(val)
	case int8:
		return int64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case uint:
		return int64(val)
	case uint8:
		return int64(val)
	case uint16:
		return int64(val)
	case uint32:
		return int64(val)
	case float32:
		return float64(val)
	default:
		return v
	}
}

func cloneRow(row map[string]any) map[string]any {
	cp := make(map[string]any, len(row))
	for k, v := range row {
		cp[k] = v
	}
	return cp
}

func inferColumnType(v any) string {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "INTEGER"
	case float32, float64:
		return "REAL"
	case bool:
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		if float64(int64(n)) == n {
			return int64(n), true
		}
		return 0, false
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int16:
		return float64(n), true
	case int8:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func matchesAll(row map[string]any, conds []Condition) bool {
	for _, c := range conds {
		val := row[c.Column]
		if !matchCondition(val, c.Op, c.Value) {
			return false
		}
	}
	return true
}

func matchCondition(actual any, op string, expected any) bool {
	switch strings.ToUpper(op) {
	case "=":
		return compareValues(actual, expected) == 0
	case "!=", "<>":
		return compareValues(actual, expected) != 0
	case ">":
		return compareValues(actual, expected) > 0
	case "<":
		return compareValues(actual, expected) < 0
	case ">=":
		return compareValues(actual, expected) >= 0
	case "<=":
		return compareValues(actual, expected) <= 0
	case "LIKE":
		if actual == nil || expected == nil {
			return false
		}
		return matchLike(fmt.Sprint(actual), fmt.Sprint(expected))
	default:
		return false
	}
}

func compareValues(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	if af, okA := toFloat64(a); okA {
		if bf, okB := toFloat64(b); okB {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	if ab, okA := a.(bool); okA {
		if bb, okB := b.(bool); okB {
			if ab == bb {
				return 0
			}
			if !ab && bb {
				return -1
			}
			return 1
		}
	}
	as := fmt.Sprint(a)
	bs := fmt.Sprint(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// matchLike performs case-insensitive SQL LIKE matching supporting '%' and '_' wildcards.
func matchLike(value, pattern string) bool {
	v := []rune(strings.ToLower(value))
	p := []rune(strings.ToLower(pattern))

	vi, pi := 0, 0
	starIdx, matchIdx := -1, 0

	for vi < len(v) {
		if pi < len(p) && (p[pi] == '_' || p[pi] == v[vi]) {
			vi++
			pi++
		} else if pi < len(p) && p[pi] == '%' {
			starIdx = pi
			matchIdx = vi
			pi++
		} else if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			vi = matchIdx
		} else {
			return false
		}
	}

	for pi < len(p) && p[pi] == '%' {
		pi++
	}
	return pi == len(p)
}
