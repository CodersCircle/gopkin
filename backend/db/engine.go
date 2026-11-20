package db

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Common errors returned by the database layer.
var (
	ErrNoConnection      = errors.New("db: no active database connection; call db.Connect first")
	ErrInvalidIdentifier = errors.New("db: invalid table or column identifier")
	ErrInvalidWhere      = errors.New("db: Where requires (column, value) or (column, operator, value)")
	ErrUnsupportedOp     = errors.New("db: unsupported filter operator")
	ErrUnauthorized      = errors.New("db: unauthorized proxy request")
)

var validIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validateIdentifier(name string) error {
	if !validIdentRe.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidIdentifier, name)
	}
	return nil
}

// Condition represents a single WHERE predicate on a column.
type Condition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// Query describes a structured table query executed by an Engine.
type Query struct {
	Table    string      `json:"table"`
	Where    []Condition `json:"where,omitempty"`
	OrderBy  string      `json:"order_by,omitempty"`
	OrderDir string      `json:"order_dir,omitempty"`
	Limit    int         `json:"limit,omitempty"`
	Offset   int         `json:"offset,omitempty"`
}

// Engine is the unified interface implemented by LocalSQL, MySQL, and MySQLProxy.
type Engine interface {
	Connect() error
	Close() error
	AutoMigrate(table string, columns map[string]string) error
	Insert(table string, data map[string]any) (int64, error)
	Find(query Query) ([]map[string]any, error)
	Update(query Query, data map[string]any) (int64, error)
	Delete(query Query) (int64, error)
	RawQuery(sql string, args ...any) ([]map[string]any, error)
}

var (
	defaultMu     sync.RWMutex
	defaultEngine Engine
)

// Connect initializes engine and registers it as the package-level default Engine.
// If another default engine was previously connected, it is closed before switching.
func Connect(engine Engine) error {
	if engine == nil {
		return errors.New("db: engine cannot be nil")
	}
	if err := engine.Connect(); err != nil {
		return err
	}

	defaultMu.Lock()
	prev := defaultEngine
	defaultEngine = engine
	defaultMu.Unlock()

	if prev != nil && prev != engine {
		_ = prev.Close()
	}
	return nil
}

// Close closes the active package-level default Engine, if any.
func Close() error {
	defaultMu.Lock()
	eng := defaultEngine
	defaultEngine = nil
	defaultMu.Unlock()

	if eng == nil {
		return nil
	}
	return eng.Close()
}

// Default returns the active package-level Engine, or nil if Connect has not been called.
func Default() Engine {
	defaultMu.RLock()
	eng := defaultEngine
	defaultMu.RUnlock()
	return eng
}

// AutoMigrate creates or updates table with the given column definitions on the default Engine.
func AutoMigrate(table string, columns map[string]string) error {
	eng := Default()
	if eng == nil {
		return ErrNoConnection
	}
	return eng.AutoMigrate(table, columns)
}

// RawQuery executes a raw SQL statement on the default Engine.
func RawQuery(sql string, args ...any) ([]map[string]any, error) {
	eng := Default()
	if eng == nil {
		return nil, ErrNoConnection
	}
	return eng.RawQuery(sql, args...)
}

// Table returns a fluent QueryBuilder targeting table on the default Engine.
func Table(table string) *QueryBuilder {
	return &QueryBuilder{
		engine: Default(),
		query:  Query{Table: table},
	}
}

// Model is an alias for Table for Filament-style declarative APIs.
func Model(table string) *QueryBuilder {
	return Table(table)
}

// From returns a fluent QueryBuilder bound to an explicit Engine instance.
func From(engine Engine, table string) *QueryBuilder {
	return &QueryBuilder{
		engine: engine,
		query:  Query{Table: table},
	}
}

// QueryBuilder provides a Fluent/Eloquent-style API for CRUD and querying.
type QueryBuilder struct {
	engine Engine
	query  Query
	err    error
}

func (qb *QueryBuilder) resolveEngine() (Engine, error) {
	if qb.err != nil {
		return nil, qb.err
	}
	if err := validateIdentifier(qb.query.Table); err != nil {
		return nil, err
	}
	eng := qb.engine
	if eng == nil {
		eng = Default()
	}
	if eng == nil {
		return nil, ErrNoConnection
	}
	return eng, nil
}

// Where appends a filter condition to the query.
// Supports both 2-argument equality form: Where("status", "on")
// and 3-argument operator form: Where("temp", ">", 25) or Where("name", "LIKE", "%Light%").
func (qb *QueryBuilder) Where(column string, args ...any) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if err := validateIdentifier(column); err != nil {
		qb.err = err
		return qb
	}

	var op string
	var val any
	switch len(args) {
	case 1:
		op = "="
		val = args[0]
	case 2:
		opStr, ok := args[0].(string)
		if !ok {
			qb.err = fmt.Errorf("%w: operator must be a string", ErrUnsupportedOp)
			return qb
		}
		op = strings.ToUpper(strings.TrimSpace(opStr))
		val = args[1]
	default:
		qb.err = ErrInvalidWhere
		return qb
	}

	switch op {
	case "=", "!=", "<>", ">", "<", ">=", "<=", "LIKE":
	default:
		qb.err = fmt.Errorf("%w: %q", ErrUnsupportedOp, op)
		return qb
	}

	qb.query.Where = append(qb.query.Where, Condition{
		Column: column,
		Op:     op,
		Value:  val,
	})
	return qb
}

// OrderBy sets the sort column and direction ("ASC" or "DESC"; defaults to "ASC").
func (qb *QueryBuilder) OrderBy(column string, direction ...string) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if err := validateIdentifier(column); err != nil {
		qb.err = err
		return qb
	}
	dir := "ASC"
	if len(direction) > 0 && strings.TrimSpace(direction[0]) != "" {
		dir = strings.ToUpper(strings.TrimSpace(direction[0]))
	}
	if dir != "ASC" && dir != "DESC" {
		qb.err = fmt.Errorf("db: invalid order direction %q (expected ASC or DESC)", dir)
		return qb
	}
	qb.query.OrderBy = column
	qb.query.OrderDir = dir
	return qb
}

// Limit caps the maximum number of rows returned.
func (qb *QueryBuilder) Limit(n int) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if n < 0 {
		n = 0
	}
	qb.query.Limit = n
	return qb
}

// Offset skips the first n matching rows.
func (qb *QueryBuilder) Offset(n int) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if n < 0 {
		n = 0
	}
	qb.query.Offset = n
	return qb
}

// AutoMigrate ensures the builder's table exists with the specified columns.
func (qb *QueryBuilder) AutoMigrate(columns map[string]string) error {
	eng, err := qb.resolveEngine()
	if err != nil {
		return err
	}
	return eng.AutoMigrate(qb.query.Table, columns)
}

// Get executes the query and returns all matching rows.
func (qb *QueryBuilder) Get() ([]map[string]any, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return nil, err
	}
	return eng.Find(qb.query)
}

// First executes the query with Limit(1) and returns the first matching row, or nil if none.
func (qb *QueryBuilder) First() (map[string]any, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return nil, err
	}
	q := qb.query
	q.Limit = 1
	rows, err := eng.Find(q)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// Count returns the number of rows matching the query's WHERE filters.
func (qb *QueryBuilder) Count() (int64, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return 0, err
	}
	q := Query{
		Table: qb.query.Table,
		Where: qb.query.Where,
	}
	rows, err := eng.Find(q)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

// Insert inserts a new row into the table and returns the assigned ID.
func (qb *QueryBuilder) Insert(data map[string]any) (int64, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return 0, err
	}
	return eng.Insert(qb.query.Table, data)
}

// Update modifies all rows matching the query's WHERE clauses and returns the affected row count.
func (qb *QueryBuilder) Update(data map[string]any) (int64, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return 0, err
	}
	return eng.Update(qb.query, data)
}

// Delete removes all rows matching the query's WHERE clauses and returns the deleted row count.
func (qb *QueryBuilder) Delete() (int64, error) {
	eng, err := qb.resolveEngine()
	if err != nil {
		return 0, err
	}
	return eng.Delete(qb.query)
}
