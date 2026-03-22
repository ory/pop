package pop

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gobuffalo/fizz"
	"github.com/gobuffalo/fizz/translators"
	"github.com/jmoiron/sqlx"
	"github.com/ory/pop/v6/columns"
	"github.com/ory/pop/v6/internal/defaults"
	"github.com/ory/pop/v6/logging"
)

const nameSQLite3 = "sqlite3"

func init() {
	AvailableDialects = append(AvailableDialects, nameSQLite3)
	dialectSynonyms["sqlite"] = nameSQLite3
	urlParser[nameSQLite3] = urlParserSQLite3
	newConnection[nameSQLite3] = newSQLite
	finalizer[nameSQLite3] = finalizerSQLite
}

var _ dialect = &sqlite{}

type sqlite struct {
	commonDialect
	gil   *sync.Mutex
	smGil *sync.Mutex
}

func requireSQLite3() error {
	for _, driverName := range sql.Drivers() {
		if driverName == nameSQLite3 {
			return nil
		}
	}
	return errors.New("sqlite3 support was not compiled into the binary")
}

func (m *sqlite) Name() string {
	return nameSQLite3
}

func (m *sqlite) DefaultDriver() string {
	return nameSQLite3
}

func (m *sqlite) Details() *ConnectionDetails {
	return m.ConnectionDetails
}

func (m *sqlite) URL() string {
	c := m.ConnectionDetails
	return c.Database + "?" + c.OptionsString("")
}

func (m *sqlite) MigrationURL() string {
	return m.ConnectionDetails.URL
}

func (m *sqlite) Create(c *Connection, model *Model, cols columns.Columns) error {
	return m.locker(m.smGil, func() error {
		keyType, err := model.PrimaryKeyType()
		if err != nil {
			return err
		}
		switch keyType {
		case "int", "int64":
			var id int64
			cols.Remove(model.IDField())
			w := cols.Writeable()
			var query string
			if len(w.Cols) > 0 {
				query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", m.Quote(model.TableName()), w.QuotedString(m), w.SymbolizedString())
			} else {
				query = fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", m.Quote(model.TableName()))
			}
			normalizeTimesToUTC(model.Value)
			txlog(logging.SQL, c, query, model.Value)
			res, err := c.Store.NamedExecContext(model.ctx, query, model.Value)
			if err != nil {
				return err
			}
			id, err = res.LastInsertId()
			if err == nil {
				model.setID(id)
			}
			if err != nil {
				return err
			}
			return nil
		}
		normalizeTimesToUTC(model.Value)
		if err := genericCreate(c, model, cols, m); err != nil {
			return fmt.Errorf("sqlite create: %w", err)
		}
		return nil
	})
}

func (m *sqlite) Update(c *Connection, model *Model, cols columns.Columns) error {
	return m.locker(m.smGil, func() error {
		normalizeTimesToUTC(model.Value)
		if err := genericUpdate(c, model, cols, m); err != nil {
			return fmt.Errorf("sqlite update: %w", err)
		}
		return nil
	})
}

func (m *sqlite) UpdateQuery(c *Connection, model *Model, cols columns.Columns, query Query) (int64, error) {
	rowsAffected := int64(0)
	err := m.locker(m.smGil, func() error {
		normalizeTimesToUTC(model.Value)
		if n, err := genericUpdateQuery(c, model, cols, m, query, sqlx.QUESTION); err != nil {
			rowsAffected = n
			return fmt.Errorf("sqlite update query: %w", err)
		} else {
			rowsAffected = n
			return nil
		}
	})
	return rowsAffected, err
}

func (m *sqlite) Destroy(c *Connection, model *Model) error {
	return m.locker(m.smGil, func() error {
		if err := genericDestroy(c, model, m); err != nil {
			return fmt.Errorf("sqlite destroy: %w", err)
		}
		return nil
	})
}

func (m *sqlite) Delete(c *Connection, model *Model, query Query) error {
	return genericDelete(c, model, query)
}

func (m *sqlite) SelectOne(c *Connection, model *Model, query Query) error {
	return m.locker(m.smGil, func() error {
		if err := genericSelectOne(c, model, query); err != nil {
			return fmt.Errorf("sqlite select one: %w", err)
		}
		normalizeTimesToUTC(model.Value)
		return nil
	})
}

func (m *sqlite) SelectMany(c *Connection, models *Model, query Query) error {
	return m.locker(m.smGil, func() error {
		if err := genericSelectMany(c, models, query); err != nil {
			return fmt.Errorf("sqlite select many: %w", err)
		}
		normalizeTimesToUTC(models.Value)
		return nil
	})
}

func (m *sqlite) Lock(fn func() error) error {
	return m.locker(m.gil, fn)
}

func (m *sqlite) locker(l *sync.Mutex, fn func() error) error {
	if defaults.String(m.Details().option("lock"), "true") == "true" {
		defer l.Unlock()
		l.Lock()
	}
	err := fn()
	attempts := 0
	for err != nil && err.Error() == "database is locked" && attempts <= m.Details().RetryLimit() {
		time.Sleep(m.Details().RetrySleep())
		err = fn()
		attempts++
	}
	return err
}

func (m *sqlite) CreateDB() error {
	durl := m.ConnectionDetails.Database

	// Checking whether the url specifies in-memory mode
	// as specified in https://github.com/mattn/go-sqlite3#faq
	if strings.Contains(durl, ":memory:") || strings.Contains(durl, "mode=memory") {
		log(logging.Info, "in memory db selected, no database file created.")

		return nil
	}

	_, err := os.Stat(durl)
	if err == nil {
		return fmt.Errorf("could not create SQLite database '%s'; database exists", durl)
	}
	dir := filepath.Dir(durl)
	err = os.MkdirAll(dir, 0766)
	if err != nil {
		return fmt.Errorf("could not create SQLite database '%s': %w", durl, err)
	}
	f, err := os.Create(durl)
	if err != nil {
		return fmt.Errorf("could not create SQLite database '%s': %w", durl, err)
	}
	_ = f.Close()

	log(logging.Info, "created database '%s'", durl)
	return nil
}

func (m *sqlite) DropDB() error {
	err := os.Remove(m.ConnectionDetails.Database)
	if err != nil {
		return fmt.Errorf("could not drop SQLite database %s: %w", m.ConnectionDetails.Database, err)
	}
	log(logging.Info, "dropped database '%s'", m.ConnectionDetails.Database)
	return nil
}

func (m *sqlite) TranslateSQL(sql string) string {
	return sql
}

func (m *sqlite) FizzTranslator() fizz.Translator {
	return translators.NewSQLite(m.Details().Database)
}

func (m *sqlite) DumpSchema(w io.Writer) error {
	cmd := exec.Command("sqlite3", m.Details().Database, ".schema")
	return genericDumpSchema(m.Details(), cmd, w)
}

func (m *sqlite) LoadSchema(r io.Reader) error {
	cmd := exec.Command("sqlite3", m.ConnectionDetails.Database)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	go func() {
		defer in.Close()
		io.Copy(in, r)
	}()
	log(logging.SQL, strings.Join(cmd.Args, " "))
	err = cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	log(logging.Info, "loaded schema for %s", m.Details().Database)
	return nil
}

func (m *sqlite) TruncateAll(tx *Connection) error {
	const tableNames = `SELECT name FROM sqlite_master WHERE type = "table"`
	names := []struct {
		Name string `db:"name"`
	}{}

	err := tx.RawQuery(tableNames).All(&names)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	stmts := []string{}
	for _, n := range names {
		stmts = append(stmts, fmt.Sprintf("DELETE FROM %s", m.Quote(n.Name)))
	}
	return tx.RawQuery(strings.Join(stmts, "; ")).Exec()
}

func newSQLite(deets *ConnectionDetails) (dialect, error) {
	err := requireSQLite3()
	if err != nil {
		return nil, err
	}
	deets.URL = fmt.Sprintf("sqlite3://%s", deets.Database)
	cd := &sqlite{
		gil:           &sync.Mutex{},
		smGil:         &sync.Mutex{},
		commonDialect: commonDialect{ConnectionDetails: deets},
	}

	return cd, nil
}

func urlParserSQLite3(cd *ConnectionDetails) error {
	db := strings.TrimPrefix(cd.URL, "sqlite://")
	db = strings.TrimPrefix(db, "sqlite3://")

	dbparts := strings.Split(db, "?")
	cd.Database = dbparts[0]

	if len(dbparts) != 2 {
		return nil
	}

	// Preserve the raw query string so finalizerSQLite can parse multi-value
	// params (e.g. multiple _pragma entries) via url.Values, which supports
	// duplicate keys. The generic withURL path sets RawOptions the same way.
	cd.RawOptions = dbparts[1]

	q, err := url.ParseQuery(dbparts[1])
	if err != nil {
		return fmt.Errorf("unable to parse sqlite query: %w", err)
	}

	for k := range q {
		cd.setOption(k, q.Get(k))
	}

	return nil
}

// legacySQLiteParams maps mattn-style DSN params to SQLite pragma names for
// modernc.org/sqlite, which requires _pragma=name(value) syntax. Aliases
// (e.g. _foreign_keys/_fk) list the canonical long form first so the first
// match wins when both aliases are present in the same DSN.
var legacySQLiteParams = []struct{ key, pragma string }{
	{"_foreign_keys", "foreign_keys"},
	{"_fk", "foreign_keys"},
	{"_journal_mode", "journal_mode"},
	{"_journal", "journal_mode"},
	{"_busy_timeout", "busy_timeout"},
	{"_timeout", "busy_timeout"},
	{"_synchronous", "synchronous"},
	{"_sync", "synchronous"},
	{"_auto_vacuum", "auto_vacuum"},
	{"_vacuum", "auto_vacuum"},
	{"_case_sensitive_like", "case_sensitive_like"},
	{"_cslike", "case_sensitive_like"},
	{"_defer_foreign_keys", "defer_foreign_keys"},
	{"_defer_fk", "defer_foreign_keys"},
	{"_locking_mode", "locking_mode"},
	{"_locking", "locking_mode"},
	{"_recursive_triggers", "recursive_triggers"},
	{"_rt", "recursive_triggers"},
	{"_cache_size", "cache_size"},
	{"_ignore_check_constraints", "ignore_check_constraints"},
	{"_query_only", "query_only"},
	{"_secure_delete", "secure_delete"},
	{"_writable_schema", "writable_schema"},
}

// sqliteInternalKeys are pop-internal connection options that must not be
// forwarded to the SQLite DSN.
var sqliteInternalKeys = map[string]bool{
	"migration_table_name": true,
	"retry_sleep":          true,
	"retry_limit":          true,
	"lock":                 true,
}

// moderncSQLiteParams is the complete set of DSN query parameters recognised
// by modernc.org/sqlite. Any key not in this set will be warned and stripped.
// Source: modernc.org/sqlite@v1.47.0/sqlite.go applyQueryParams() and
// modernc.org/sqlite@v1.47.0/conn.go newConn().
var moderncSQLiteParams = map[string]bool{
	"vfs":                  true, // VFS name
	"_pragma":              true, // PRAGMA name(value); repeatable
	"_time_format":         true, // time write format; only "sqlite" is valid
	"_txlock":              true, // transaction locking: deferred/immediate/exclusive
	"_time_integer_format": true, // integer time repr: unix/unix_milli/unix_micro/unix_nano
	"_inttotime":           true, // convert integer columns to time.Time
	"_texttotime":          true, // affect ColumnTypeScanType for TEXT date columns
}

func finalizerSQLite(cd *ConnectionDetails) {
	// modernc.org/sqlite (registered as "sqlite3") requires pragmas via
	// _pragma=name(value) DSN params. Legacy mattn-style params are silently
	// ignored by modernc and must be translated.

	// Build url.Values from RawOptions (set when a DSN URL was parsed) or the
	// Options map (set programmatically). url.Values supports duplicate keys,
	// which is required for multiple _pragma entries.
	var q url.Values
	if cd.RawOptions != "" {
		var err error
		q, err = url.ParseQuery(cd.RawOptions)
		if err != nil {
			q = url.Values{}
		}
	} else {
		q = url.Values{}
		for k, v := range cd.Options {
			if !sqliteInternalKeys[k] {
				q.Set(k, v)
			}
		}
	}

	// _loc is a mattn-only timezone param with no modernc equivalent.
	// modernc returns time.UTC natively; use _time_format if a different format is needed.
	if q.Get("_loc") != "" {
		log(logging.Warn, "SQLite DSN param \"_loc\" has no modernc.org/sqlite equivalent and will be ignored")
		q.Del("_loc")
	}

	// Translate all legacy mattn-style params to _pragma=name(value).
	for _, p := range legacySQLiteParams {
		if val := q.Get(p.key); val != "" {
			q.Del(p.key)
			if !sqlitePragmaSet(q, p.pragma) {
				q.Add("_pragma", p.pragma+"("+val+")")
			}
		}
	}

	// Strip any remaining keys that modernc.org/sqlite does not recognise.
	for k := range q {
		if !moderncSQLiteParams[k] {
			log(logging.Warn, "SQLite DSN param %q is not supported by modernc.org/sqlite and will be ignored", k)
			q.Del(k)
			delete(cd.Options, k)
		}
	}

	// Default to mattn-compatible on-disk format for correct lexicographic ordering.
	if q.Get("_time_format") == "" {
		q.Set("_time_format", "sqlite")
		cd.setOption("_time_format", "sqlite")
	}

	// Apply default busy_timeout if not configured.
	if !sqlitePragmaSet(q, "busy_timeout") {
		q.Add("_pragma", "busy_timeout(5000)")
	}
	// Enforce foreign_keys.
	if !sqlitePragmaSet(q, "foreign_keys") {
		q.Add("_pragma", "foreign_keys(1)")
		if cd.URL != "" {
			log(logging.Warn, "IMPORTANT! '_pragma=foreign_keys(1)' is required for correct operation. Add it to your SQLite DSN.")
		}
	}
	cd.RawOptions = q.Encode()

	// Reflect all applied pragmas back into cd.Options for backward-compatible
	// reads via cd.option(). sqliteOptionKey maps pragma names to their preferred
	// option key; everything else uses "_"+pragmaName.
	for _, pragma := range q["_pragma"] {
		rawName, rawValue, ok := strings.Cut(pragma, "(")
		if !ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(rawName))
		value := strings.TrimSuffix(strings.TrimSpace(rawValue), ")")
		key := "_" + name
		if name == "foreign_keys" {
			key = "_fk"
		}
		cd.setOption(key, value)
	}
}

// sqlitePragmaSet reports whether q already contains a _pragma entry for
// pragmaName (case-insensitive). Requires the pragma name to be immediately
// followed by '(' to avoid false matches on names sharing a common prefix
// (e.g. "foreign_keys" vs "foreign_keys_per_table").
func sqlitePragmaSet(q url.Values, pragmaName string) bool {
	prefix := strings.ToLower(pragmaName) + "("
	for _, p := range q["_pragma"] {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p)), prefix) {
			return true
		}
	}
	return false
}

var (
	typeTime     = reflect.TypeFor[time.Time]()
	typeNullTime = reflect.TypeFor[sql.NullTime]()
)

// normalizeTimesToUTC walks v (a pointer to a struct or pointer to a slice of
// structs) and calls .UTC() on every time.Time and valid sql.NullTime field,
// including those inside embedded structs and behind pointer fields.
// This is required because modernc.org/sqlite may return time.Time values
// whose Location pointer is not time.UTC even when the stored instant is UTC
// (e.g. unnamed FixedZone("", 0) from rows written by mattn/go-sqlite3).
func normalizeTimesToUTC(v any) {
	normalizeValue(reflect.ValueOf(v))
}

func normalizeValue(rv reflect.Value) {
	// Dereference any pointer indirection (handles nil safely).
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			normalizeValue(rv.Index(i))
		}
	case reflect.Struct:
		for i := range rv.NumField() {
			f := rv.Field(i)
			if !f.CanSet() {
				continue
			}
			// Dereference one pointer level so *time.Time and *Struct are
			// handled the same as their value equivalents.
			target := f
			if target.Kind() == reflect.Pointer {
				if target.IsNil() {
					continue
				}
				target = target.Elem()
			}
			switch target.Type() {
			case typeTime:
				target.Set(reflect.ValueOf(target.Interface().(time.Time).UTC()))
			case typeNullTime:
				nt := target.Interface().(sql.NullTime)
				if nt.Valid {
					nt.Time = nt.Time.UTC()
					target.Set(reflect.ValueOf(nt))
				}
			default:
				normalizeValue(target)
			}
		}
	}
}

func newSQLiteDriver() (driver.Driver, error) {
	err := requireSQLite3()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(nameSQLite3, ":memory:?cache=newSQLiteDriver_temporary")
	if err != nil {
		return nil, err
	}
	return db.Driver(), db.Close()
}
