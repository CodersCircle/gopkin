package db_test

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkin/backend/db"
)

func TestLocalSQLPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app_data.db")

	if err := db.Connect(db.LocalSQL(dbPath)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	id1, err := db.Table("devices").Insert(map[string]any{
		"name":   "Light 1",
		"status": "on",
		"watts":  12,
	})
	if err != nil {
		t.Fatalf("Insert 1 failed: %v", err)
	}
	id2, err := db.Table("devices").Insert(map[string]any{
		"name":   "Fan 1",
		"status": "off",
		"watts":  45,
	})
	if err != nil {
		t.Fatalf("Insert 2 failed: %v", err)
	}
	if id1 != 1 || id2 != 2 {
		t.Fatalf("expected auto-increment IDs 1, 2; got %d, %d", id1, id2)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Ensure atomic rename left no stray temporary files behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "app_data.db" {
		t.Fatalf("expected single file app_data.db, got %v", entries)
	}

	// Reopen from disk and verify persisted rows & auto-increment continuity.
	if err := db.Connect(db.LocalSQL(dbPath)); err != nil {
		t.Fatalf("Reopen Connect failed: %v", err)
	}
	defer db.Close()

	rows, err := db.Table("devices").OrderBy("id", "ASC").Get()
	if err != nil {
		t.Fatalf("Get after reopen failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows after reopen, got %d", len(rows))
	}
	if rows[0]["name"] != "Light 1" || rows[0]["id"] != int64(1) || rows[0]["watts"] != int64(12) {
		t.Fatalf("unexpected row 0 after reopen: %#v", rows[0])
	}
	if rows[1]["name"] != "Fan 1" || rows[1]["id"] != int64(2) {
		t.Fatalf("unexpected row 1 after reopen: %#v", rows[1])
	}

	id3, err := db.Table("devices").Insert(map[string]any{"name": "Heater", "status": "off", "watts": 1200})
	if err != nil {
		t.Fatalf("Insert 3 after reopen failed: %v", err)
	}
	if id3 != 3 {
		t.Fatalf("expected next auto-increment ID 3 after reopen, got %d", id3)
	}
}

func TestAutoCreateAndMissingTablesNeverCrash(t *testing.T) {
	dir := t.TempDir()
	eng := db.LocalSQL(filepath.Join(dir, "auto.db"))
	if err := db.Connect(eng); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer db.Close()

	// Querying/updating/deleting a table that doesn't exist yet must not error.
	rows, err := db.Table("nonexistent").Where("status", "on").Get()
	if err != nil {
		t.Fatalf("Get on missing table returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows on missing table, got %d", len(rows))
	}

	updated, err := db.Table("nonexistent").Where("id", 1).Update(map[string]any{"status": "off"})
	if err != nil || updated != 0 {
		t.Fatalf("Update on missing table: got (%d, %v), want (0, nil)", updated, err)
	}

	deleted, err := db.Table("nonexistent").Where("id", 1).Delete()
	if err != nil || deleted != 0 {
		t.Fatalf("Delete on missing table: got (%d, %v), want (0, nil)", deleted, err)
	}

	// AutoMigrate followed by Insert.
	if err := db.AutoMigrate("sensors", map[string]string{"label": "TEXT", "temp": "REAL"}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	id, err := db.Table("sensors").Insert(map[string]any{"label": "Living Room", "temp": 23.5})
	if err != nil || id != 1 {
		t.Fatalf("Insert after AutoMigrate: got (%d, %v), want (1, nil)", id, err)
	}
}

func TestQueryBuilderFiltersSortingPaginationCRUD(t *testing.T) {
	eng := db.LocalSQL(":memory:")
	if err := db.Connect(eng); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer db.Close()

	seed := []map[string]any{
		{"name": "Living Light", "status": "on", "watts": 10},
		{"name": "Kitchen Light", "status": "on", "watts": 18},
		{"name": "Garage Fan", "status": "off", "watts": 60},
		{"name": "Bedroom Light", "status": "on", "watts": 8},
		{"name": "Porch Sensor", "status": "on", "watts": 3},
	}
	for _, item := range seed {
		if _, err := db.Table("devices").Insert(item); err != nil {
			t.Fatalf("seed insert failed: %v", err)
		}
	}

	// Equality + OrderBy DESC + Limit.
	rows, err := db.Table("devices").Where("status", "on").OrderBy("id", "DESC").Limit(2).Get()
	if err != nil {
		t.Fatalf("Where/OrderBy/Limit failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Porch Sensor" || rows[1]["name"] != "Bedroom Light" {
		t.Fatalf("unexpected DESC+Limit result: %#v", rows)
	}

	// Offset + Limit pagination.
	page2, err := db.Table("devices").Where("status", "on").OrderBy("id", "ASC").Offset(1).Limit(2).Get()
	if err != nil {
		t.Fatalf("Pagination failed: %v", err)
	}
	if len(page2) != 2 || page2[0]["name"] != "Kitchen Light" || page2[1]["name"] != "Bedroom Light" {
		t.Fatalf("unexpected page2 result: %#v", page2)
	}

	// Comparison operators: >, <, !=.
	heavy, err := db.Table("devices").Where("watts", ">", 15).OrderBy("watts", "ASC").Get()
	if err != nil {
		t.Fatalf("Where > failed: %v", err)
	}
	if len(heavy) != 2 || heavy[0]["name"] != "Kitchen Light" || heavy[1]["name"] != "Garage Fan" {
		t.Fatalf("unexpected > result: %#v", heavy)
	}

	low, err := db.Table("devices").Where("watts", "<", 10).Where("status", "!=", "off").OrderBy("watts", "ASC").Get()
	if err != nil {
		t.Fatalf("Where < and != failed: %v", err)
	}
	if len(low) != 2 || low[0]["name"] != "Porch Sensor" || low[1]["name"] != "Bedroom Light" {
		t.Fatalf("unexpected < and != result: %#v", low)
	}

	// LIKE operator (case-insensitive with % and _).
	lights, err := db.Table("devices").Where("name", "LIKE", "%light").OrderBy("id", "ASC").Get()
	if err != nil {
		t.Fatalf("Where LIKE failed: %v", err)
	}
	if len(lights) != 3 {
		t.Fatalf("expected 3 lights, got %d: %#v", len(lights), lights)
	}

	// Count & First.
	cnt, err := db.Table("devices").Where("status", "on").Count()
	if err != nil || cnt != 4 {
		t.Fatalf("expected count 4, got (%d, %v)", cnt, err)
	}
	first, err := db.Table("devices").Where("id", 1).First()
	if err != nil || first == nil || first["name"] != "Living Light" {
		t.Fatalf("unexpected First result: %#v, err=%v", first, err)
	}

	// Update.
	affected, err := db.Table("devices").Where("id", 1).Update(map[string]any{"status": "off"})
	if err != nil || affected != 1 {
		t.Fatalf("Update failed: affected=%d err=%v", affected, err)
	}
	updatedRow, err := db.Table("devices").Where("id", 1).First()
	if err != nil || updatedRow["status"] != "off" {
		t.Fatalf("expected updated status 'off', got %#v (err=%v)", updatedRow, err)
	}

	// Delete.
	deleted, err := db.Table("devices").Where("id", 1).Delete()
	if err != nil || deleted != 1 {
		t.Fatalf("Delete failed: deleted=%d err=%v", deleted, err)
	}
	remaining, err := db.Table("devices").Count()
	if err != nil || remaining != 4 {
		t.Fatalf("expected 4 remaining rows, got (%d, %v)", remaining, err)
	}

	// RawQuery on LocalSQL.
	rawRows, err := db.RawQuery("SELECT name, watts FROM devices WHERE status = ? ORDER BY watts DESC LIMIT 2", "on")
	if err != nil {
		t.Fatalf("RawQuery SELECT failed: %v", err)
	}
	if len(rawRows) != 2 || rawRows[0]["name"] != "Kitchen Light" {
		t.Fatalf("unexpected RawQuery result: %#v", rawRows)
	}
}

func TestLocalSQLConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	eng := db.LocalSQL(filepath.Join(dir, "concurrent.db"))
	if err := eng.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer eng.Close()

	const workers = 8
	const perWorker = 5
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if _, err := db.From(eng, "events").Insert(map[string]any{
					"worker": workerID,
					"seq":    i,
				}); err != nil {
					errs <- err
					return
				}
				if _, err := db.From(eng, "events").Where("worker", workerID).Get(); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent operation failed: %v", err)
	}

	total, err := db.From(eng, "events").Count()
	if err != nil || total != workers*perWorker {
		t.Fatalf("expected %d rows, got (%d, %v)", workers*perWorker, total, err)
	}
}

func TestProxyHTTPRoundTrip(t *testing.T) {
	backing := db.LocalSQL(filepath.Join(t.TempDir(), "proxy_backend.db"))
	if err := backing.Connect(); err != nil {
		t.Fatalf("backing Connect failed: %v", err)
	}
	defer backing.Close()

	const secretToken = "gopkin-secret-token-123"
	srv := httptest.NewServer(db.NewProxyHandler(backing, secretToken))
	defer srv.Close()

	// Wrong token must fail with ErrUnauthorized.
	badProxy := db.MySQLProxy(srv.URL, "wrong-token")
	if err := badProxy.Connect(); !errors.Is(err, db.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for bad token, got %v", err)
	}

	// Valid token connects and supports full fluent QueryBuilder API.
	if err := db.Connect(db.MySQLProxy(srv.URL, secretToken)); err != nil {
		t.Fatalf("Proxy Connect failed: %v", err)
	}
	defer db.Close()

	if err := db.AutoMigrate("devices", map[string]string{"name": "TEXT", "status": "TEXT", "watts": "INTEGER"}); err != nil {
		t.Fatalf("Proxy AutoMigrate failed: %v", err)
	}

	id1, err := db.Table("devices").Insert(map[string]any{"name": "Light 1", "status": "on", "watts": 12})
	if err != nil || id1 != 1 {
		t.Fatalf("Proxy Insert 1 failed: id=%d err=%v", id1, err)
	}
	id2, err := db.Table("devices").Insert(map[string]any{"name": "Light 2", "status": "on", "watts": 24})
	if err != nil || id2 != 2 {
		t.Fatalf("Proxy Insert 2 failed: id=%d err=%v", id2, err)
	}
	_, err = db.Table("devices").Insert(map[string]any{"name": "AC Unit", "status": "off", "watts": 1500})
	if err != nil {
		t.Fatalf("Proxy Insert 3 failed: %v", err)
	}

	rows, err := db.Table("devices").Where("status", "on").OrderBy("id", "DESC").Limit(10).Get()
	if err != nil {
		t.Fatalf("Proxy Find failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Light 2" || rows[1]["name"] != "Light 1" {
		t.Fatalf("unexpected Proxy Find rows: %#v", rows)
	}

	affected, err := db.Table("devices").Where("id", 1).Update(map[string]any{"status": "off"})
	if err != nil || affected != 1 {
		t.Fatalf("Proxy Update failed: affected=%d err=%v", affected, err)
	}

	deleted, err := db.Table("devices").Where("id", 2).Delete()
	if err != nil || deleted != 1 {
		t.Fatalf("Proxy Delete failed: deleted=%d err=%v", deleted, err)
	}

	rawRows, err := db.RawQuery("SELECT name, status FROM devices ORDER BY id ASC")
	if err != nil {
		t.Fatalf("Proxy RawQuery failed: %v", err)
	}
	if len(rawRows) != 2 || rawRows[0]["status"] != "off" || rawRows[1]["name"] != "AC Unit" {
		t.Fatalf("unexpected Proxy RawQuery rows: %#v", rawRows)
	}
}

func TestParseMySQLDSN(t *testing.T) {
	cfg, err := db.ParseMySQLDSN("alice:s3cr3t@tcp(127.0.0.1:3306)/myapp?timeout=3s")
	if err != nil {
		t.Fatalf("ParseMySQLDSN failed: %v", err)
	}
	if cfg.User != "alice" || cfg.Password != "s3cr3t" || cfg.Network != "tcp" ||
		cfg.Addr != "127.0.0.1:3306" || cfg.DBName != "myapp" {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestMySQLWireProtocolClient(t *testing.T) {
	const expectedUser = "gopkin"
	const expectedPass = "secret123"
	const expectedDB = "mydb"
	scramble := []byte("12345678901234567890")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock mysql failed: %v", err)
	}
	defer ln.Close()

	var serverErr error
	var queries []string
	var serverWG sync.WaitGroup
	serverWG.Add(1)

	go func() {
		defer serverWG.Done()
		conn, err := ln.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer conn.Close()

		br := bufio.NewReader(conn)
		bw := bufio.NewWriter(conn)
		writePkt := func(seq byte, payload []byte) error {
			hdr := [4]byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), seq}
			if _, err := bw.Write(hdr[:]); err != nil {
				return err
			}
			if _, err := bw.Write(payload); err != nil {
				return err
			}
			return bw.Flush()
		}
		readPkt := func() ([]byte, byte, error) {
			var hdr [4]byte
			if _, err := io.ReadFull(br, hdr[:]); err != nil {
				return nil, 0, err
			}
			n := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16
			buf := make([]byte, n)
			if _, err := io.ReadFull(br, buf); err != nil {
				return nil, 0, err
			}
			return buf, hdr[3], nil
		}

		// 1. Send HandshakeV10 packet.
		var hs bytes.Buffer
		hs.WriteByte(10) // protocol version 10
		hs.WriteString("8.0.36-gopkin-mock\x00")
		binary.Write(&hs, binary.LittleEndian, uint32(42)) // connection id
		hs.Write(scramble[:8])
		hs.WriteByte(0x00)                                        // filler
		binary.Write(&hs, binary.LittleEndian, uint16(0xffff))    // cap lower
		hs.WriteByte(0x2d)                                        // charset
		binary.Write(&hs, binary.LittleEndian, uint16(0x0002))    // status
		binary.Write(&hs, binary.LittleEndian, uint16(0x0008))    // cap upper (CLIENT_PLUGIN_AUTH)
		hs.WriteByte(21)                                          // auth plugin data len
		hs.Write(make([]byte, 10))                                // reserved
		hs.Write(scramble[8:20])                                  // part 2
		hs.WriteByte(0x00)                                        // NUL terminator
		hs.WriteString("mysql_native_password\x00")               // auth plugin name
		if err := writePkt(0, hs.Bytes()); err != nil {
			serverErr = err
			return
		}

		// 2. Read HandshakeResponse41 and verify mysql_native_password token.
		authPkt, seq, err := readPkt()
		if err != nil {
			serverErr = err
			return
		}
		pos := 32 // skip caps(4)+maxPkt(4)+charset(1)+reserved(23)
		nul := bytes.IndexByte(authPkt[pos:], 0)
		user := string(authPkt[pos : pos+nul])
		pos += nul + 1
		authLen := int(authPkt[pos])
		pos++
		authResp := authPkt[pos : pos+authLen]

		// Compute expected mysql_native_password scramble.
		s1 := sha1.Sum([]byte(expectedPass))
		s2 := sha1.Sum(s1[:])
		h := sha1.New()
		h.Write(scramble)
		h.Write(s2[:])
		s3 := h.Sum(nil)
		wantAuth := make([]byte, len(s1))
		for i := range s1 {
			wantAuth[i] = s1[i] ^ s3[i]
		}
		if user != expectedUser || !bytes.Equal(authResp, wantAuth) {
			_ = writePkt(seq+1, buildErrPkt(1045, "28000", "Access denied"))
			serverErr = fmt.Errorf("auth mismatch: user=%q", user)
			return
		}
		// Send OK packet.
		if err := writePkt(seq+1, []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}); err != nil {
			serverErr = err
			return
		}

		// 3. Handle COM_QUERY / COM_QUIT loop.
		for {
			pkt, qSeq, err := readPkt()
			if err != nil {
				return
			}
			if len(pkt) == 0 || pkt[0] == 0x01 { // COM_QUIT
				return
			}
			if pkt[0] != 0x03 { // COM_QUERY
				continue
			}
			sqlStr := string(pkt[1:])
			queries = append(queries, sqlStr)

			switch {
			case strings.HasPrefix(sqlStr, "INSERT INTO"):
				// OK packet: affected=1, last_insert_id=7
				_ = writePkt(qSeq+1, []byte{0x00, 0x01, 0x07, 0x02, 0x00, 0x00, 0x00})
			case strings.HasPrefix(sqlStr, "SELECT "):
				// ResultSet with 2 columns: id (LONGLONG=0x08), name (VAR_STRING=0xfd)
				_ = writePkt(qSeq+1, []byte{0x02})
				_ = writePkt(qSeq+2, buildColDefPkt("id", 0x08))
				_ = writePkt(qSeq+3, buildColDefPkt("name", 0xfd))
				_ = writePkt(qSeq+4, []byte{0xfe, 0x00, 0x00, 0x02, 0x00}) // EOF
				_ = writePkt(qSeq+5, buildRowPkt("7", "Light 1"))
				_ = writePkt(qSeq+6, []byte{0xfe, 0x00, 0x00, 0x02, 0x00}) // EOF
			case strings.HasPrefix(sqlStr, "UPDATE "), strings.HasPrefix(sqlStr, "DELETE "):
				// OK packet: affected=1, last_insert_id=0
				_ = writePkt(qSeq+1, []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00})
			default:
				_ = writePkt(qSeq+1, []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00})
			}
		}
	}()

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", expectedUser, expectedPass, ln.Addr().String(), expectedDB)
	mysqlEng := db.MySQL(dsn)
	if err := db.Connect(mysqlEng); err != nil {
		t.Fatalf("MySQL Connect failed: %v", err)
	}

	id, err := db.Table("devices").Insert(map[string]any{"name": "Light 1", "status": "on"})
	if err != nil || id != 7 {
		t.Fatalf("MySQL Insert failed: id=%d err=%v", id, err)
	}

	rows, err := db.Table("devices").Where("status", "on").OrderBy("id", "DESC").Limit(10).Get()
	if err != nil {
		t.Fatalf("MySQL Find failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != int64(7) || rows[0]["name"] != "Light 1" {
		t.Fatalf("unexpected MySQL Find rows: %#v", rows)
	}

	upd, err := db.Table("devices").Where("id", 7).Update(map[string]any{"status": "off"})
	if err != nil || upd != 1 {
		t.Fatalf("MySQL Update failed: upd=%d err=%v", upd, err)
	}

	del, err := db.Table("devices").Where("id", 7).Delete()
	if err != nil || del != 1 {
		t.Fatalf("MySQL Delete failed: del=%d err=%v", del, err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("MySQL Close failed: %v", err)
	}
	serverWG.Wait()

	if serverErr != nil {
		t.Fatalf("mock MySQL server error: %v", serverErr)
	}
	if len(queries) != 4 {
		t.Fatalf("expected 4 COM_QUERY statements, got %d: %v", len(queries), queries)
	}
}

func buildErrPkt(code uint16, sqlState, msg string) []byte {
	var b bytes.Buffer
	b.WriteByte(0xff)
	binary.Write(&b, binary.LittleEndian, code)
	b.WriteByte('#')
	b.WriteString(sqlState)
	b.WriteString(msg)
	return b.Bytes()
}

func writeLenEncStr(b *bytes.Buffer, s string) {
	b.WriteByte(byte(len(s)))
	b.WriteString(s)
}

func buildColDefPkt(name string, colType byte) []byte {
	var b bytes.Buffer
	writeLenEncStr(&b, "def")
	writeLenEncStr(&b, "mydb")
	writeLenEncStr(&b, "devices")
	writeLenEncStr(&b, "devices")
	writeLenEncStr(&b, name)
	writeLenEncStr(&b, name)
	b.WriteByte(0x0c)                                     // fixed len
	binary.Write(&b, binary.LittleEndian, uint16(0x2d))   // charset
	binary.Write(&b, binary.LittleEndian, uint32(255))    // column length
	b.WriteByte(colType)                                  // column type
	binary.Write(&b, binary.LittleEndian, uint16(0x0000)) // flags
	b.WriteByte(0x00)                                     // decimals
	b.Write([]byte{0x00, 0x00})                           // filler
	return b.Bytes()
}

func buildRowPkt(vals ...string) []byte {
	var b bytes.Buffer
	for _, v := range vals {
		writeLenEncStr(&b, v)
	}
	return b.Bytes()
}
