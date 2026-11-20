package db

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type proxyRequest struct {
	Action  string            `json:"action"`
	Table   string            `json:"table,omitempty"`
	Columns map[string]string `json:"columns,omitempty"`
	Data    map[string]any    `json:"data,omitempty"`
	Query   *Query            `json:"query,omitempty"`
	SQL     string            `json:"sql,omitempty"`
	Args    []any             `json:"args,omitempty"`
}

type proxyResponse struct {
	OK       bool             `json:"ok"`
	ID       int64            `json:"id,omitempty"`
	Affected int64            `json:"affected,omitempty"`
	Rows     []map[string]any `json:"rows,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// ProxyEngine implements Engine over an authenticated HTTP/JSON bridge so public
// mobile/web apps can query live MySQL without embedding database credentials.
type ProxyEngine struct {
	endpoint string
	token    string
	client   *http.Client
}

// MySQLProxy creates a new ProxyEngine targeting endpoint with Bearer token authentication.
// Example: db.MySQLProxy("https://db-bridge.myapp.com", "API_TOKEN")
func MySQLProxy(endpoint, token string) *ProxyEngine {
	return &ProxyEngine{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:    token,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Connect verifies connectivity and token authentication with the proxy server.
func (p *ProxyEngine) Connect() error {
	if p.endpoint == "" {
		return errors.New("db: proxy endpoint cannot be empty")
	}
	_, err := p.do(proxyRequest{Action: "ping"})
	return err
}

// Close closes any idle HTTP connections held by the proxy client.
func (p *ProxyEngine) Close() error {
	if p.client != nil {
		p.client.CloseIdleConnections()
	}
	return nil
}

// AutoMigrate forwards schema migration to the proxy server.
func (p *ProxyEngine) AutoMigrate(table string, columns map[string]string) error {
	_, err := p.do(proxyRequest{
		Action:  "automigrate",
		Table:   table,
		Columns: columns,
	})
	return err
}

// Insert forwards an insert operation to the proxy server and returns the inserted ID.
func (p *ProxyEngine) Insert(table string, data map[string]any) (int64, error) {
	resp, err := p.do(proxyRequest{
		Action: "insert",
		Table:  table,
		Data:   data,
	})
	if err != nil {
		return 0, err
	}
	return resp.ID, nil
}

// Find forwards a structured query to the proxy server and returns matching rows.
func (p *ProxyEngine) Find(query Query) ([]map[string]any, error) {
	q := query
	resp, err := p.do(proxyRequest{
		Action: "find",
		Query:  &q,
	})
	if err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []map[string]any{}, nil
	}
	return resp.Rows, nil
}

// Update forwards an update operation to the proxy server and returns the affected row count.
func (p *ProxyEngine) Update(query Query, data map[string]any) (int64, error) {
	q := query
	resp, err := p.do(proxyRequest{
		Action: "update",
		Query:  &q,
		Data:   data,
	})
	if err != nil {
		return 0, err
	}
	return resp.Affected, nil
}

// Delete forwards a delete operation to the proxy server and returns the deleted row count.
func (p *ProxyEngine) Delete(query Query) (int64, error) {
	q := query
	resp, err := p.do(proxyRequest{
		Action: "delete",
		Query:  &q,
	})
	if err != nil {
		return 0, err
	}
	return resp.Affected, nil
}

// RawQuery forwards a raw SQL query to the proxy server.
func (p *ProxyEngine) RawQuery(sql string, args ...any) ([]map[string]any, error) {
	resp, err := p.do(proxyRequest{
		Action: "raw",
		SQL:    sql,
		Args:   args,
	})
	if err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []map[string]any{}, nil
	}
	return resp.Rows, nil
}

func (p *ProxyEngine) do(reqBody proxyRequest) (*proxyResponse, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("db: marshal proxy request: %w", err)
	}

	url := p.endpoint + "/db/exec"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("db: build proxy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("db: proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("db: read proxy response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var out proxyResponse
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("db: decode proxy response (status %d): %w", resp.StatusCode, err)
	}

	if !out.OK {
		if out.Error != "" {
			return nil, errors.New(out.Error)
		}
		return nil, fmt.Errorf("db: proxy returned status %d", resp.StatusCode)
	}

	for i, row := range out.Rows {
		out.Rows[i] = normalizeRow(row)
	}
	return &out, nil
}

// NewProxyHandler returns an http.Handler that exposes engine over the Gopkin
// HTTP/JSON proxy protocol protected by Bearer token authentication.
func NewProxyHandler(engine Engine, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeProxyJSON(w, http.StatusMethodNotAllowed, proxyResponse{
				OK:    false,
				Error: "method not allowed; use POST",
			})
			return
		}

		if token != "" {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + token
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) != 1 {
				writeProxyJSON(w, http.StatusUnauthorized, proxyResponse{
					OK:    false,
					Error: ErrUnauthorized.Error(),
				})
				return
			}
		}

		dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024*1024))
		dec.UseNumber()

		var req proxyRequest
		if err := dec.Decode(&req); err != nil {
			writeProxyJSON(w, http.StatusBadRequest, proxyResponse{
				OK:    false,
				Error: fmt.Sprintf("invalid JSON body: %v", err),
			})
			return
		}

		if req.Data != nil {
			req.Data = normalizeRow(req.Data)
		}
		if req.Query != nil {
			for i := range req.Query.Where {
				req.Query.Where[i].Value = normalizeValue(req.Query.Where[i].Value)
			}
		}
		for i := range req.Args {
			req.Args[i] = normalizeValue(req.Args[i])
		}

		switch req.Action {
		case "ping":
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true})
		case "automigrate":
			if err := engine.AutoMigrate(req.Table, req.Columns); err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true})
		case "insert":
			id, err := engine.Insert(req.Table, req.Data)
			if err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true, ID: id})
		case "find":
			if req.Query == nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: "missing query"})
				return
			}
			rows, err := engine.Find(*req.Query)
			if err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true, Rows: rows})
		case "update":
			if req.Query == nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: "missing query"})
				return
			}
			affected, err := engine.Update(*req.Query, req.Data)
			if err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true, Affected: affected})
		case "delete":
			if req.Query == nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: "missing query"})
				return
			}
			affected, err := engine.Delete(*req.Query)
			if err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true, Affected: affected})
		case "raw":
			rows, err := engine.RawQuery(req.SQL, req.Args...)
			if err != nil {
				writeProxyJSON(w, http.StatusBadRequest, proxyResponse{OK: false, Error: err.Error()})
				return
			}
			writeProxyJSON(w, http.StatusOK, proxyResponse{OK: true, Rows: rows})
		default:
			writeProxyJSON(w, http.StatusBadRequest, proxyResponse{
				OK:    false,
				Error: fmt.Sprintf("unknown proxy action %q", req.Action),
			})
		}
	})
}

func writeProxyJSON(w http.ResponseWriter, status int, resp proxyResponse) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// NewProxyServer creates a configured *http.Server serving the proxy bridge for engine.
func NewProxyServer(addr string, engine Engine, token string) *http.Server {
	mux := http.NewServeMux()
	handler := NewProxyHandler(engine, token)
	mux.Handle("/db/exec", handler)
	mux.Handle("/", handler)

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// StartProxyServer connects to MySQL using mysqlDSN and starts the authenticated
// HTTP/JSON proxy server on addr.
func StartProxyServer(addr, mysqlDSN, token string) error {
	engine := MySQL(mysqlDSN)
	if err := engine.Connect(); err != nil {
		return fmt.Errorf("db: proxy failed to connect to MySQL: %w", err)
	}
	defer engine.Close()

	srv := NewProxyServer(addr, engine, token)
	return srv.ListenAndServe()
}
