// Package sqlitedriver provides a minimal database/sql driver for SQLite3 using CGO.
// It links against the system libsqlite3 which includes FTS5 support.
package sqlitedriver

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unsafe"
)

func init() {
	sql.Register("sqlite3", &SQLiteDriver{})
}

// SQLiteDriver implements database/sql/driver.Driver.
type SQLiteDriver struct{}

func (d *SQLiteDriver) Open(dsn string) (driver.Conn, error) {
	// Split DSN into path and params: "path?params"
	path := dsn
	var params string
	if idx := strings.IndexByte(dsn, '?'); idx >= 0 {
		path = dsn[:idx]
		params = dsn[idx+1:]
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var db *C.sqlite3
	rc := C.sqlite3_open_v2(cPath, &db,
		C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX,
		nil)
	if rc != C.SQLITE_OK {
		if db != nil {
			C.sqlite3_close(db)
		}
		return nil, fmt.Errorf("sqlite3_open: %s", C.GoString(C.sqlite3_errmsg(db)))
	}

	conn := &sqliteConn{db: db}

	// Apply pragma parameters
	if err := conn.applyParams(params); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

type sqliteConn struct {
	db *C.sqlite3
	mu sync.Mutex
}

func (c *sqliteConn) applyParams(params string) error {
	if params == "" {
		return nil
	}
	for _, param := range strings.Split(params, "&") {
		if !strings.HasPrefix(param, "_") {
			continue
		}
		// Parse _key=value -> PRAGMA key = value
		kv := strings.SplitN(param[1:], "=", 2)
		if len(kv) != 2 {
			continue
		}
		pragma := fmt.Sprintf("PRAGMA %s = %s", kv[0], kv[1])
		if err := c.exec(pragma); err != nil {
			return fmt.Errorf("setting %s: %w", kv[0], err)
		}
	}
	return nil
}

func (c *sqliteConn) exec(sql string) error {
	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))

	var errMsg *C.char
	rc := C.sqlite3_exec(c.db, cSQL, nil, nil, &errMsg)
	if rc != C.SQLITE_OK {
		msg := C.GoString(errMsg)
		C.sqlite3_free(unsafe.Pointer(errMsg))
		return fmt.Errorf("sqlite3_exec: %s", msg)
	}
	return nil
}

// ExecContext implements driver.ExecerContext. It uses sqlite3_exec for
// statements without parameters, which supports multi-statement SQL
// (required for migrations with CREATE TRIGGER ... BEGIN ... END).
func (c *sqliteConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) == 0 {
		c.mu.Lock()
		defer c.mu.Unlock()

		if err := c.exec(query); err != nil {
			return nil, err
		}
		lastID := int64(C.sqlite3_last_insert_rowid(c.db))
		changes := int64(C.sqlite3_changes(c.db))
		return &sqliteResult{lastID: lastID, changes: changes}, nil
	}

	// Fall back to prepare+exec for parameterized queries.
	stmt, err := c.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	values := make([]driver.Value, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	return stmt.Exec(values)
}

func (c *sqliteConn) Prepare(query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))

	var stmt *C.sqlite3_stmt
	rc := C.sqlite3_prepare_v2(c.db, cQuery, C.int(len(query)), &stmt, nil)
	if rc != C.SQLITE_OK {
		return nil, fmt.Errorf("sqlite3_prepare: %s", C.GoString(C.sqlite3_errmsg(c.db)))
	}

	return &sqliteStmt{conn: c, stmt: stmt, query: query}, nil
}

func (c *sqliteConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	rc := C.sqlite3_close(c.db)
	if rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite3_close: %s", C.GoString(C.sqlite3_errmsg(c.db)))
	}
	c.db = nil
	return nil
}

func (c *sqliteConn) Begin() (driver.Tx, error) {
	if err := c.exec("BEGIN"); err != nil {
		return nil, err
	}
	return &sqliteTx{conn: c}, nil
}

type sqliteTx struct {
	conn *sqliteConn
}

func (tx *sqliteTx) Commit() error {
	return tx.conn.exec("COMMIT")
}

func (tx *sqliteTx) Rollback() error {
	return tx.conn.exec("ROLLBACK")
}

type sqliteStmt struct {
	conn  *sqliteConn
	stmt  *C.sqlite3_stmt
	query string
}

func (s *sqliteStmt) Close() error {
	rc := C.sqlite3_finalize(s.stmt)
	if rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite3_finalize: %s", C.GoString(C.sqlite3_errmsg(s.conn.db)))
	}
	s.stmt = nil
	return nil
}

func (s *sqliteStmt) NumInput() int {
	return int(C.sqlite3_bind_parameter_count(s.stmt))
}

func (s *sqliteStmt) bind(args []driver.Value) error {
	for i, arg := range args {
		idx := C.int(i + 1) // SQLite binds are 1-indexed
		var rc C.int

		switch v := arg.(type) {
		case nil:
			rc = C.sqlite3_bind_null(s.stmt, idx)
		case int64:
			rc = C.sqlite3_bind_int64(s.stmt, idx, C.sqlite3_int64(v))
		case float64:
			rc = C.sqlite3_bind_double(s.stmt, idx, C.double(v))
		case bool:
			if v {
				rc = C.sqlite3_bind_int64(s.stmt, idx, 1)
			} else {
				rc = C.sqlite3_bind_int64(s.stmt, idx, 0)
			}
		case string:
			cStr := C.CString(v)
			rc = C.sqlite3_bind_text(s.stmt, idx, cStr, C.int(len(v)), (*[0]byte)(C.free))
		case time.Time:
			ts := v.Format("2006-01-02T15:04:05.000Z07:00")
			cStr := C.CString(ts)
			rc = C.sqlite3_bind_text(s.stmt, idx, cStr, C.int(len(ts)), (*[0]byte)(C.free))
		case []byte:
			if len(v) == 0 {
				rc = C.sqlite3_bind_zeroblob(s.stmt, idx, 0)
			} else {
				rc = C.sqlite3_bind_blob(s.stmt, idx, unsafe.Pointer(&v[0]), C.int(len(v)), nil)
			}
		default:
			return fmt.Errorf("unsupported bind type: %T", arg)
		}

		if rc != C.SQLITE_OK {
			return fmt.Errorf("sqlite3_bind: %s", C.GoString(C.sqlite3_errmsg(s.conn.db)))
		}
	}
	return nil
}

func (s *sqliteStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	C.sqlite3_reset(s.stmt)
	C.sqlite3_clear_bindings(s.stmt)

	if err := s.bind(args); err != nil {
		return nil, err
	}

	rc := C.sqlite3_step(s.stmt)
	if rc != C.SQLITE_DONE && rc != C.SQLITE_ROW {
		return nil, fmt.Errorf("sqlite3_step: %s", C.GoString(C.sqlite3_errmsg(s.conn.db)))
	}

	lastID := int64(C.sqlite3_last_insert_rowid(s.conn.db))
	changes := int64(C.sqlite3_changes(s.conn.db))

	return &sqliteResult{lastID: lastID, changes: changes}, nil
}

func (s *sqliteStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.mu.Lock()
	// Note: unlock happens when rows are closed

	C.sqlite3_reset(s.stmt)
	C.sqlite3_clear_bindings(s.stmt)

	if err := s.bind(args); err != nil {
		s.conn.mu.Unlock()
		return nil, err
	}

	colCount := int(C.sqlite3_column_count(s.stmt))
	cols := make([]string, colCount)
	for i := 0; i < colCount; i++ {
		cols[i] = C.GoString(C.sqlite3_column_name(s.stmt, C.int(i)))
	}

	return &sqliteRows{stmt: s, cols: cols}, nil
}

type sqliteResult struct {
	lastID  int64
	changes int64
}

func (r *sqliteResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r *sqliteResult) RowsAffected() (int64, error) { return r.changes, nil }

type sqliteRows struct {
	stmt   *sqliteStmt
	cols   []string
	closed bool
}

func (r *sqliteRows) Columns() []string {
	return r.cols
}

func (r *sqliteRows) Close() error {
	if !r.closed {
		r.closed = true
		C.sqlite3_reset(r.stmt.stmt)
		r.stmt.conn.mu.Unlock()
	}
	return nil
}

func (r *sqliteRows) Next(dest []driver.Value) error {
	rc := C.sqlite3_step(r.stmt.stmt)
	if rc == C.SQLITE_DONE {
		return io.EOF
	}
	if rc != C.SQLITE_ROW {
		return fmt.Errorf("sqlite3_step: %s", C.GoString(C.sqlite3_errmsg(r.stmt.conn.db)))
	}

	for i := range dest {
		ci := C.int(i)
		colType := C.sqlite3_column_type(r.stmt.stmt, ci)

		switch colType {
		case C.SQLITE_NULL:
			dest[i] = nil
		case C.SQLITE_INTEGER:
			dest[i] = int64(C.sqlite3_column_int64(r.stmt.stmt, ci))
		case C.SQLITE_FLOAT:
			dest[i] = float64(C.sqlite3_column_double(r.stmt.stmt, ci))
		case C.SQLITE_TEXT:
			n := C.sqlite3_column_bytes(r.stmt.stmt, ci)
			p := C.sqlite3_column_text(r.stmt.stmt, ci)
			s := C.GoStringN((*C.char)(unsafe.Pointer(p)), n)
			// Try to parse as time if it looks like a timestamp
			if t, err := parseTime(s); err == nil {
				dest[i] = t
			} else {
				dest[i] = s
			}
		case C.SQLITE_BLOB:
			n := C.sqlite3_column_bytes(r.stmt.stmt, ci)
			p := C.sqlite3_column_blob(r.stmt.stmt, ci)
			if n > 0 {
				dest[i] = C.GoBytes(p, n)
			} else {
				dest[i] = []byte{}
			}
		default:
			return errors.New("unknown column type")
		}
	}
	return nil
}

// timeFormats are the formats attempted when parsing TEXT columns as time.Time.
var timeFormats = []string{
	"2006-01-02T15:04:05.000Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	// Quick check: timestamps start with a digit and are at least 10 chars
	if len(s) < 10 || s[0] < '0' || s[0] > '9' {
		return time.Time{}, errors.New("not a timestamp")
	}
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("not a timestamp")
}
