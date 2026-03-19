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
		if err := genericCreate(c, model, cols, m); err != nil {
			return fmt.Errorf("sqlite create: %w", err)
		}
		return nil
	})
}

func (m *sqlite) Update(c *Connection, model *Model, cols columns.Columns) error {
	return m.locker(m.smGil, func() error {
		if err := genericUpdate(c, model, cols, m); err != nil {
			return fmt.Errorf("sqlite update: %w", err)
		}
		return nil
	})
}

func (m *sqlite) UpdateQuery(c *Connection, model *Model, cols columns.Columns, query Query) (int64, error) {
	rowsAffected := int64(0)
	err := m.locker(m.smGil, func() error {
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
		return nil
	})
}

func (m *sqlite) SelectMany(c *Connection, models *Model, query Query) error {
	return m.locker(m.smGil, func() error {
		if err := genericSelectMany(c, models, query); err != nil {
			return fmt.Errorf("sqlite select many: %w", err)
		}
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

	q, err := url.ParseQuery(dbparts[1])
	if err != nil {
		return fmt.Errorf("unable to parse sqlite query: %w", err)
	}

	for k := range q {
		cd.setOption(k, q.Get(k))
	}

	return nil
}

func finalizerSQLite(cd *ConnectionDetails) {
	// modernc.org/sqlite (registered as "sqlite3") requires pragmas via
	// _pragma=name(value) DSN params. Legacy mattn-style params (_fk,
	// _journal_mode, _busy_timeout) are silently ignored and must be translated.

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
		popInternal := map[string]bool{
			"migration_table_name": true,
			"retry_sleep":          true,
			"retry_limit":          true,
			"lock":                 true,
		}
		for k, v := range cd.Options {
			if !popInternal[k] {
				q.Set(k, v)
			}
		}
	}

	// Translate legacy mattn-style params to _pragma equivalents.
	for _, t := range []struct{ key, pragma string }{
		{"_fk", "foreign_keys"},
		{"_journal_mode", "journal_mode"},
		{"_busy_timeout", "busy_timeout"},
	} {
		if val := q.Get(t.key); val != "" {
			q.Del(t.key)
			if !sqlitePragmaSet(q, t.pragma) {
				q.Add("_pragma", t.pragma+"("+val+")")
			}
		}
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
	// Reflect applied defaults back into Options (legacy keys) for backward compatibility.
	cd.setOptionWithDefault("_busy_timeout", cd.option("_busy_timeout"), "5000")
	cd.setOptionWithDefault("_fk", cd.option("_fk"), "true")
}

// sqlitePragmaSet reports whether q already contains a _pragma entry whose
// name starts with pragmaName (case-insensitive).
func sqlitePragmaSet(q url.Values, pragmaName string) bool {
	prefix := strings.ToLower(pragmaName)
	for _, p := range q["_pragma"] {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p)), prefix) {
			return true
		}
	}
	return false
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
