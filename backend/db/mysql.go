package db

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MySQL wire-protocol capability flags.
const (
	mysqlClientLongPassword     uint32 = 1 << 0
	mysqlClientFoundRows        uint32 = 1 << 1
	mysqlClientLongFlag         uint32 = 1 << 2
	mysqlClientConnectWithDB    uint32 = 1 << 3
	mysqlClientProtocol41       uint32 = 1 << 9
	mysqlClientTransactions     uint32 = 1 << 13
	mysqlClientSecureConnection uint32 = 1 << 15
	mysqlClientPluginAuth       uint32 = 1 << 19
)

// MySQL wire-protocol command and packet headers.
const (
	mysqlComQuit  byte = 0x01
	mysqlComQuery byte = 0x03

	mysqlPktOK   byte = 0x00
	mysqlPktMore byte = 0x01
	mysqlPktEOF  byte = 0xfe
	mysqlPktErr  byte = 0xff
)

// MySQL column type constants for result decoding.
const (
	mysqlTypeDecimal    byte = 0x00
	mysqlTypeTiny       byte = 0x01
	mysqlTypeShort      byte = 0x02
	mysqlTypeLong       byte = 0x03
	mysqlTypeFloat      byte = 0x04
	mysqlTypeDouble     byte = 0x05
	mysqlTypeLongLong   byte = 0x08
	mysqlTypeInt24      byte = 0x09
	mysqlTypeNewDecimal byte = 0xf6
)

// MySQLConfig holds parsed connection parameters from a MySQL DSN.
type MySQLConfig struct {
	User     string
	Password string
	Network  string
	Addr     string
	DBName   string
	Timeout  time.Duration
}

// ParseMySQLDSN parses a standard MySQL DSN of the form:
// "user:pass@tcp(127.0.0.1:3306)/mydb"
func ParseMySQLDSN(dsn string) (MySQLConfig, error) {
	cfg := MySQLConfig{
		Network: "tcp",
		Addr:    "127.0.0.1:3306",
		Timeout: 10 * time.Second,
	}

	s := strings.TrimSpace(dsn)
	if s == "" {
		return cfg, errors.New("db: empty MySQL DSN")
	}

	// Strip optional query params (e.g. ?timeout=5s).
	if qIdx := strings.Index(s, "?"); qIdx >= 0 {
		params := s[qIdx+1:]
		s = s[:qIdx]
		for _, kv := range strings.Split(params, "&") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 && parts[0] == "timeout" {
				if d, err := time.ParseDuration(parts[1]); err == nil && d > 0 {
					cfg.Timeout = d
				}
			}
		}
	}

	// Split credentials and host/db on the last '@'.
	if atIdx := strings.LastIndex(s, "@"); atIdx >= 0 {
		creds := s[:atIdx]
		s = s[atIdx+1:]
		if colonIdx := strings.Index(creds, ":"); colonIdx >= 0 {
			cfg.User = creds[:colonIdx]
			cfg.Password = creds[colonIdx+1:]
		} else {
			cfg.User = creds
		}
	}

	// Split network(addr) and /dbname.
	slashIdx := strings.Index(s, "/")
	var hostPart string
	if slashIdx >= 0 {
		hostPart = s[:slashIdx]
		cfg.DBName = s[slashIdx+1:]
	} else {
		hostPart = s
	}

	if hostPart != "" {
		if openIdx := strings.Index(hostPart, "("); openIdx >= 0 && strings.HasSuffix(hostPart, ")") {
			cfg.Network = hostPart[:openIdx]
			cfg.Addr = hostPart[openIdx+1 : len(hostPart)-1]
		} else {
			cfg.Addr = hostPart
		}
	}

	if cfg.Addr == "" {
		return cfg, errors.New("db: missing host address in MySQL DSN")
	}
	return cfg, nil
}

// MySQLError represents an ERR_Packet returned by a MySQL server.
type MySQLError struct {
	Code     uint16
	SQLState string
	Message  string
}

func (e *MySQLError) Error() string {
	if e.SQLState != "" {
		return fmt.Sprintf("mysql error %d (%s): %s", e.Code, e.SQLState, e.Message)
	}
	return fmt.Sprintf("mysql error %d: %s", e.Code, e.Message)
}

// MySQLEngine is a lightweight Pure-Go MySQL wire-protocol client built solely on Go stdlib.
type MySQLEngine struct {
	mu      sync.Mutex
	dsn     string
	cfg     MySQLConfig
	conn    net.Conn
	br      *bufio.Reader
	bw      *bufio.Writer
	seq     byte
	capFlag uint32
}

// MySQL creates a new Pure-Go MySQLEngine configured with dsn.
// Example: db.MySQL("root:secret@tcp(127.0.0.1:3306)/myapp")
func MySQL(dsn string) *MySQLEngine {
	return &MySQLEngine{dsn: dsn}
}

// Connect dials the MySQL server, completes the HandshakeV10 exchange, and authenticates.
func (m *MySQLEngine) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := ParseMySQLDSN(m.dsn)
	if err != nil {
		return err
	}
	m.cfg = cfg

	conn, err := net.DialTimeout(cfg.Network, cfg.Addr, cfg.Timeout)
	if err != nil {
		return fmt.Errorf("db: dial mysql %s: %w", cfg.Addr, err)
	}
	m.conn = conn
	m.br = bufio.NewReaderSize(conn, 16*1024)
	m.bw = bufio.NewWriterSize(conn, 16*1024)
	m.seq = 0

	if err := m.handshakeLocked(); err != nil {
		_ = m.conn.Close()
		m.conn = nil
		return err
	}
	return nil
}

// Close sends COM_QUIT and closes the underlying TCP connection.
func (m *MySQLEngine) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil {
		return nil
	}
	m.seq = 0
	_ = m.writePacketLocked([]byte{mysqlComQuit})
	err := m.conn.Close()
	m.conn = nil
	return err
}

// AutoMigrate creates the table if it does not already exist.
func (m *MySQLEngine) AutoMigrate(table string, columns map[string]string) error {
	if err := validateIdentifier(table); err != nil {
		return err
	}
	colNames := make([]string, 0, len(columns))
	for col := range columns {
		if err := validateIdentifier(col); err != nil {
			return err
		}
		if col != "id" {
			colNames = append(colNames, col)
		}
	}
	sort.Strings(colNames)

	defs := []string{"`id` BIGINT AUTO_INCREMENT PRIMARY KEY"}
	for _, col := range colNames {
		defs = append(defs, fmt.Sprintf("`%s` %s", col, mysqlColumnType(columns[col])))
	}

	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (%s)", table, strings.Join(defs, ", "))
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _, _, err := m.execQueryLocked(stmt)
	return err
}

// Insert inserts a row into table and returns the last insert ID.
// If the table does not exist (MySQL error 1146), it auto-creates the table and retries once.
func (m *MySQLEngine) Insert(table string, data map[string]any) (int64, error) {
	if err := validateIdentifier(table); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, errors.New("db: insert data cannot be empty")
	}

	cols := make([]string, 0, len(data))
	for k := range data {
		if err := validateIdentifier(k); err != nil {
			return 0, err
		}
		cols = append(cols, k)
	}
	sort.Strings(cols)

	quotedCols := make([]string, len(cols))
	vals := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = "`" + c + "`"
		vals[i] = formatSQLLiteral(data[c])
	}

	stmt := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		table, strings.Join(quotedCols, ", "), strings.Join(vals, ", "))

	m.mu.Lock()
	_, lastID, _, err := m.execQueryLocked(stmt)
	m.mu.Unlock()

	var myErr *MySQLError
	if errors.As(err, &myErr) && myErr.Code == 1146 {
		inferred := make(map[string]string, len(data))
		for k, v := range data {
			inferred[k] = inferColumnType(normalizeValue(v))
		}
		if migErr := m.AutoMigrate(table, inferred); migErr != nil {
			return 0, migErr
		}
		m.mu.Lock()
		_, lastID, _, err = m.execQueryLocked(stmt)
		m.mu.Unlock()
	}
	return lastID, err
}

// Find executes a structured SELECT query on MySQL.
func (m *MySQLEngine) Find(query Query) ([]map[string]any, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("SELECT * FROM `")
	b.WriteString(query.Table)
	b.WriteByte('`')

	whereSQL, err := buildWhereClause(query.Where)
	if err != nil {
		return nil, err
	}
	if whereSQL != "" {
		b.WriteString(" WHERE ")
		b.WriteString(whereSQL)
	}

	if query.OrderBy != "" {
		if err := validateIdentifier(query.OrderBy); err != nil {
			return nil, err
		}
		dir := "ASC"
		if strings.EqualFold(query.OrderDir, "DESC") {
			dir = "DESC"
		}
		b.WriteString(" ORDER BY `")
		b.WriteString(query.OrderBy)
		b.WriteString("` ")
		b.WriteString(dir)
	}

	if query.Limit > 0 {
		b.WriteString(" LIMIT ")
		b.WriteString(strconv.Itoa(query.Limit))
	} else if query.Offset > 0 {
		// MySQL requires LIMIT when OFFSET is specified.
		b.WriteString(" LIMIT 18446744073709551615")
	}
	if query.Offset > 0 {
		b.WriteString(" OFFSET ")
		b.WriteString(strconv.Itoa(query.Offset))
	}

	m.mu.Lock()
	rows, _, _, err := m.execQueryLocked(b.String())
	m.mu.Unlock()

	var myErr *MySQLError
	if errors.As(err, &myErr) && myErr.Code == 1146 {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}

// Update modifies matching rows in MySQL and returns the affected row count.
func (m *MySQLEngine) Update(query Query, data map[string]any) (int64, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}

	cols := make([]string, 0, len(data))
	for k := range data {
		if err := validateIdentifier(k); err != nil {
			return 0, err
		}
		cols = append(cols, k)
	}
	sort.Strings(cols)

	setParts := make([]string, len(cols))
	for i, c := range cols {
		setParts[i] = fmt.Sprintf("`%s` = %s", c, formatSQLLiteral(data[c]))
	}

	var b strings.Builder
	b.WriteString("UPDATE `")
	b.WriteString(query.Table)
	b.WriteString("` SET ")
	b.WriteString(strings.Join(setParts, ", "))

	whereSQL, err := buildWhereClause(query.Where)
	if err != nil {
		return 0, err
	}
	if whereSQL != "" {
		b.WriteString(" WHERE ")
		b.WriteString(whereSQL)
	}

	m.mu.Lock()
	_, _, affected, err := m.execQueryLocked(b.String())
	m.mu.Unlock()

	var myErr *MySQLError
	if errors.As(err, &myErr) && myErr.Code == 1146 {
		return 0, nil
	}
	return affected, err
}

// Delete removes matching rows in MySQL and returns the affected row count.
func (m *MySQLEngine) Delete(query Query) (int64, error) {
	if err := validateIdentifier(query.Table); err != nil {
		return 0, err
	}

	var b strings.Builder
	b.WriteString("DELETE FROM `")
	b.WriteString(query.Table)
	b.WriteByte('`')

	whereSQL, err := buildWhereClause(query.Where)
	if err != nil {
		return 0, err
	}
	if whereSQL != "" {
		b.WriteString(" WHERE ")
		b.WriteString(whereSQL)
	}

	m.mu.Lock()
	_, _, affected, err := m.execQueryLocked(b.String())
	m.mu.Unlock()

	var myErr *MySQLError
	if errors.As(err, &myErr) && myErr.Code == 1146 {
		return 0, nil
	}
	return affected, err
}

// RawQuery sends a COM_QUERY statement directly to the MySQL server.
func (m *MySQLEngine) RawQuery(sqlStr string, args ...any) ([]map[string]any, error) {
	stmt, err := interpolatePlaceholders(sqlStr, args)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	rows, lastID, affected, err := m.execQueryLocked(stmt)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if rows != nil {
		return rows, nil
	}
	return []map[string]any{{
		"affected_rows":  affected,
		"last_insert_id": lastID,
	}}, nil
}

// --- MySQL Wire Protocol Implementation ---

func (m *MySQLEngine) handshakeLocked() error {
	pkt, err := m.readPacketLocked()
	if err != nil {
		return fmt.Errorf("db: read mysql handshake: %w", err)
	}
	if len(pkt) == 0 {
		return errors.New("db: empty mysql handshake packet")
	}
	if pkt[0] == mysqlPktErr {
		return parseMySQLErrPacket(pkt)
	}
	if pkt[0] != 10 {
		return fmt.Errorf("db: unsupported mysql protocol version %d", pkt[0])
	}

	pos := 1
	// Skip NUL-terminated server version string.
	nulIdx := bytes.IndexByte(pkt[pos:], 0)
	if nulIdx < 0 {
		return errors.New("db: malformed mysql handshake (server version)")
	}
	pos += nulIdx + 1

	// Connection ID (4 bytes) + auth_plugin_data_part_1 (8 bytes) + filler (1 byte).
	if pos+13 > len(pkt) {
		return errors.New("db: malformed mysql handshake (truncated header)")
	}
	pos += 4 // connection ID
	scramble := make([]byte, 0, 20)
	scramble = append(scramble, pkt[pos:pos+8]...)
	pos += 8
	pos++ // filler

	// Capability flags lower 2 bytes.
	if pos+2 > len(pkt) {
		return errors.New("db: malformed mysql handshake (capabilities)")
	}
	serverCaps := uint32(binary.LittleEndian.Uint16(pkt[pos : pos+2]))
	pos += 2

	authPlugin := "mysql_native_password"
	if pos < len(pkt) {
		// charset (1) + status (2) + cap_upper (2) + auth_len (1) + reserved (10) = 16 bytes
		if pos+16 <= len(pkt) {
			pos++ // charset
			pos += 2
			serverCaps |= uint32(binary.LittleEndian.Uint16(pkt[pos:pos+2])) << 16
			pos += 2
			authDataLen := int(pkt[pos])
			pos++
			pos += 10 // reserved

			if serverCaps&mysqlClientSecureConnection != 0 && pos < len(pkt) {
				part2Len := 13
				if authDataLen-8 > part2Len {
					part2Len = authDataLen - 8
				}
				if pos+part2Len > len(pkt) {
					part2Len = len(pkt) - pos
				}
				part2 := pkt[pos : pos+part2Len]
				part2 = bytes.TrimRight(part2, "\x00")
				scramble = append(scramble, part2...)
				pos += part2Len
			}
			if serverCaps&mysqlClientPluginAuth != 0 && pos < len(pkt) {
				end := bytes.IndexByte(pkt[pos:], 0)
				if end >= 0 {
					authPlugin = string(pkt[pos : pos+end])
				} else {
					authPlugin = string(pkt[pos:])
				}
			}
		}
	}

	clientFlags := mysqlClientProtocol41 |
		mysqlClientSecureConnection |
		mysqlClientLongPassword |
		mysqlClientTransactions |
		mysqlClientLongFlag |
		mysqlClientPluginAuth
	if m.cfg.DBName != "" {
		clientFlags |= mysqlClientConnectWithDB
	}
	m.capFlag = clientFlags & serverCaps

	authResp := computeMySQLAuth(authPlugin, m.cfg.Password, scramble)

	var resp bytes.Buffer
	var hdr [9]byte
	binary.LittleEndian.PutUint32(hdr[0:4], clientFlags)
	binary.LittleEndian.PutUint32(hdr[4:8], 16*1024*1024) // max packet size
	hdr[8] = 0x2d                                        // utf8mb4_general_ci
	resp.Write(hdr[:])
	resp.Write(make([]byte, 23)) // reserved

	resp.WriteString(m.cfg.User)
	resp.WriteByte(0)

	resp.WriteByte(byte(len(authResp)))
	resp.Write(authResp)

	if m.cfg.DBName != "" {
		resp.WriteString(m.cfg.DBName)
		resp.WriteByte(0)
	}
	resp.WriteString(authPlugin)
	resp.WriteByte(0)

	if err := m.writePacketLocked(resp.Bytes()); err != nil {
		return fmt.Errorf("db: send mysql handshake response: %w", err)
	}

	return m.readAuthResultLocked(authPlugin)
}

func (m *MySQLEngine) readAuthResultLocked(currentPlugin string) error {
	for {
		pkt, err := m.readPacketLocked()
		if err != nil {
			return fmt.Errorf("db: read mysql auth response: %w", err)
		}
		if len(pkt) == 0 {
			return errors.New("db: empty mysql auth response")
		}
		switch pkt[0] {
		case mysqlPktOK:
			return nil
		case mysqlPktErr:
			return parseMySQLErrPacket(pkt)
		case mysqlPktEOF:
			// AuthSwitchRequest: 0xfe + plugin_name + NUL + scramble
			if len(pkt) <= 1 {
				return errors.New("db: unexpected EOF during mysql auth")
			}
			nulIdx := bytes.IndexByte(pkt[1:], 0)
			if nulIdx < 0 {
				return errors.New("db: malformed mysql auth switch request")
			}
			newPlugin := string(pkt[1 : 1+nulIdx])
			newScramble := bytes.TrimRight(pkt[1+nulIdx+1:], "\x00")
			authResp := computeMySQLAuth(newPlugin, m.cfg.Password, newScramble)
			if err := m.writePacketLocked(authResp); err != nil {
				return fmt.Errorf("db: write mysql auth switch response: %w", err)
			}
			currentPlugin = newPlugin
		case mysqlPktMore:
			// caching_sha2_password fast auth success (0x01 0x03) -> read trailing OK packet.
			if len(pkt) >= 2 && pkt[1] == 0x03 {
				continue
			}
			return fmt.Errorf("db: mysql auth plugin %q requires TLS or full auth exchange", currentPlugin)
		default:
			return fmt.Errorf("db: unexpected mysql auth packet header 0x%02x", pkt[0])
		}
	}
}

func computeMySQLAuth(plugin, password string, scramble []byte) []byte {
	if password == "" {
		return nil
	}
	switch plugin {
	case "caching_sha2_password":
		return scrambleCachingSHA2(password, scramble)
	default:
		return scrambleNativePassword(password, scramble)
	}
}

// scrambleNativePassword implements mysql_native_password:
// SHA1(password) XOR SHA1(scramble + SHA1(SHA1(password)))
func scrambleNativePassword(password string, scramble []byte) []byte {
	stage1 := sha1.Sum([]byte(password))
	stage2 := sha1.Sum(stage1[:])

	h := sha1.New()
	h.Write(scramble)
	h.Write(stage2[:])
	stage3 := h.Sum(nil)

	out := make([]byte, len(stage1))
	for i := range stage1 {
		out[i] = stage1[i] ^ stage3[i]
	}
	return out
}

// scrambleCachingSHA2 implements caching_sha2_password fast-auth token:
// SHA256(password) XOR SHA256(SHA256(SHA256(password)) + scramble)
func scrambleCachingSHA2(password string, scramble []byte) []byte {
	stage1 := sha256.Sum256([]byte(password))
	stage2 := sha256.Sum256(stage1[:])

	h := sha256.New()
	h.Write(stage2[:])
	h.Write(scramble)
	stage3 := h.Sum(nil)

	out := make([]byte, len(stage1))
	for i := range stage1 {
		out[i] = stage1[i] ^ stage3[i]
	}
	return out
}

func (m *MySQLEngine) execQueryLocked(stmt string) ([]map[string]any, int64, int64, error) {
	if m.conn == nil {
		return nil, 0, 0, ErrNoConnection
	}
	m.seq = 0

	payload := make([]byte, 1+len(stmt))
	payload[0] = mysqlComQuery
	copy(payload[1:], stmt)

	if err := m.writePacketLocked(payload); err != nil {
		return nil, 0, 0, fmt.Errorf("db: write COM_QUERY: %w", err)
	}

	pkt, err := m.readPacketLocked()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("db: read COM_QUERY response: %w", err)
	}
	if len(pkt) == 0 {
		return nil, 0, 0, errors.New("db: empty mysql query response")
	}

	switch pkt[0] {
	case mysqlPktOK:
		affected, n := readLenEncInt(pkt[1:])
		lastID, _ := readLenEncInt(pkt[1+n:])
		return nil, int64(lastID), int64(affected), nil
	case mysqlPktErr:
		return nil, 0, 0, parseMySQLErrPacket(pkt)
	}

	// ResultSet response.
	colCount, _ := readLenEncInt(pkt)
	type colMeta struct {
		name    string
		colType byte
	}
	cols := make([]colMeta, 0, colCount)
	for i := uint64(0); i < colCount; i++ {
		colPkt, err := m.readPacketLocked()
		if err != nil {
			return nil, 0, 0, fmt.Errorf("db: read column definition: %w", err)
		}
		name, colType := parseColumnDef41(colPkt)
		cols = append(cols, colMeta{name: name, colType: colType})
	}

	// EOF packet terminating column definitions.
	eofPkt, err := m.readPacketLocked()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("db: read column EOF: %w", err)
	}
	if len(eofPkt) > 0 && eofPkt[0] == mysqlPktErr {
		return nil, 0, 0, parseMySQLErrPacket(eofPkt)
	}

	// Read result rows until EOF.
	rows := make([]map[string]any, 0)
	for {
		rowPkt, err := m.readPacketLocked()
		if err != nil {
			return nil, 0, 0, fmt.Errorf("db: read result row: %w", err)
		}
		if len(rowPkt) == 0 {
			break
		}
		if rowPkt[0] == mysqlPktErr {
			return nil, 0, 0, parseMySQLErrPacket(rowPkt)
		}
		if rowPkt[0] == mysqlPktEOF && len(rowPkt) < 9 {
			break
		}

		row := make(map[string]any, len(cols))
		pos := 0
		for _, col := range cols {
			if pos >= len(rowPkt) {
				row[col.name] = nil
				continue
			}
			if rowPkt[pos] == 0xfb {
				row[col.name] = nil
				pos++
				continue
			}
			strLen, n := readLenEncInt(rowPkt[pos:])
			pos += n
			end := pos + int(strLen)
			if end > len(rowPkt) {
				end = len(rowPkt)
			}
			raw := string(rowPkt[pos:end])
			pos = end
			row[col.name] = decodeMySQLValue(raw, col.colType)
		}
		rows = append(rows, row)
	}

	return rows, 0, int64(len(rows)), nil
}

func (m *MySQLEngine) readPacketLocked() ([]byte, error) {
	var full []byte
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(m.br, hdr[:]); err != nil {
			return nil, err
		}
		length := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16
		m.seq = hdr[3] + 1

		chunk := make([]byte, length)
		if _, err := io.ReadFull(m.br, chunk); err != nil {
			return nil, err
		}
		full = append(full, chunk...)
		if length < 0xffffff {
			break
		}
	}
	return full, nil
}

func (m *MySQLEngine) writePacketLocked(payload []byte) error {
	const maxChunk = 0xffffff
	for {
		chunkLen := len(payload)
		if chunkLen > maxChunk {
			chunkLen = maxChunk
		}
		hdr := [4]byte{
			byte(chunkLen),
			byte(chunkLen >> 8),
			byte(chunkLen >> 16),
			m.seq,
		}
		m.seq++
		if _, err := m.bw.Write(hdr[:]); err != nil {
			return err
		}
		if chunkLen > 0 {
			if _, err := m.bw.Write(payload[:chunkLen]); err != nil {
				return err
			}
		}
		payload = payload[chunkLen:]
		if chunkLen < maxChunk {
			break
		}
	}
	return m.bw.Flush()
}

func parseMySQLErrPacket(pkt []byte) error {
	if len(pkt) < 3 {
		return &MySQLError{Message: "malformed error packet"}
	}
	code := binary.LittleEndian.Uint16(pkt[1:3])
	pos := 3
	sqlState := ""
	if pos < len(pkt) && pkt[pos] == '#' && pos+6 <= len(pkt) {
		sqlState = string(pkt[pos+1 : pos+6])
		pos += 6
	}
	msg := ""
	if pos < len(pkt) {
		msg = string(pkt[pos:])
	}
	return &MySQLError{
		Code:     code,
		SQLState: sqlState,
		Message:  msg,
	}
}

func readLenEncInt(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch b[0] {
	case 0xfc:
		if len(b) < 3 {
			return 0, len(b)
		}
		return uint64(binary.LittleEndian.Uint16(b[1:3])), 3
	case 0xfd:
		if len(b) < 4 {
			return 0, len(b)
		}
		return uint64(b[1]) | uint64(b[2])<<8 | uint64(b[3])<<16, 4
	case 0xfe:
		if len(b) < 9 {
			return 0, len(b)
		}
		return binary.LittleEndian.Uint64(b[1:9]), 9
	default:
		return uint64(b[0]), 1
	}
}

func skipLenEncStr(b []byte, pos int) (string, int) {
	if pos >= len(b) {
		return "", pos
	}
	strLen, n := readLenEncInt(b[pos:])
	pos += n
	end := pos + int(strLen)
	if end > len(b) {
		end = len(b)
	}
	return string(b[pos:end]), end
}

func parseColumnDef41(pkt []byte) (string, byte) {
	pos := 0
	_, pos = skipLenEncStr(pkt, pos) // catalog
	_, pos = skipLenEncStr(pkt, pos) // schema
	_, pos = skipLenEncStr(pkt, pos) // table
	_, pos = skipLenEncStr(pkt, pos) // org_table
	name, pos := skipLenEncStr(pkt, pos)
	_, pos = skipLenEncStr(pkt, pos) // org_name

	// Next: length of fixed-length fields (0x0c), charset (2), column_length (4), type (1).
	var colType byte
	if pos+8 <= len(pkt) {
		colType = pkt[pos+7]
	}
	return name, colType
}

func decodeMySQLValue(raw string, colType byte) any {
	switch colType {
	case mysqlTypeTiny, mysqlTypeShort, mysqlTypeLong, mysqlTypeLongLong, mysqlTypeInt24:
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	case mysqlTypeFloat, mysqlTypeDouble, mysqlTypeDecimal, mysqlTypeNewDecimal:
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	}
	return raw
}

func buildWhereClause(conds []Condition) (string, error) {
	if len(conds) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		if err := validateIdentifier(c.Column); err != nil {
			return "", err
		}
		op := strings.ToUpper(strings.TrimSpace(c.Op))
		switch op {
		case "=", "!=", "<>", ">", "<", ">=", "<=", "LIKE":
		default:
			return "", fmt.Errorf("%w: %q", ErrUnsupportedOp, c.Op)
		}
		if c.Value == nil {
			if op == "=" {
				parts = append(parts, fmt.Sprintf("`%s` IS NULL", c.Column))
			} else {
				parts = append(parts, fmt.Sprintf("`%s` IS NOT NULL", c.Column))
			}
			continue
		}
		parts = append(parts, fmt.Sprintf("`%s` %s %s", c.Column, op, formatSQLLiteral(c.Value)))
	}
	return strings.Join(parts, " AND "), nil
}

func formatSQLLiteral(v any) string {
	switch val := normalizeValue(v).(type) {
	case nil:
		return "NULL"
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "1"
		}
		return "0"
	default:
		s := fmt.Sprint(val)
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `'`, `\'`)
		s = strings.ReplaceAll(s, "\x00", `\0`)
		return "'" + s + "'"
	}
}

func mysqlColumnType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "INT", "INTEGER", "BIGINT":
		return "BIGINT"
	case "REAL", "FLOAT", "DOUBLE":
		return "DOUBLE"
	case "BOOL", "BOOLEAN":
		return "TINYINT(1)"
	case "TEXT", "STRING", "VARCHAR", "":
		return "VARCHAR(255)"
	default:
		return t
	}
}
