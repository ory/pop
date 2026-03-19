package pop

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var sqliteDefaultOptions = map[string]string{"_busy_timeout": "5000", "_fk": "1"}

func Test_ConnectionDetails_Finalize_SQLite_URL_Only(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite3:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: N/A")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite3:///tmp/foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: sqlite3:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_OverrideOptions_URL_Only(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite3:///tmp/foo.db?_fk=false&foo=bar",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: N/A")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite3:///tmp/foo.db?_fk=false&foo=bar")
	r.EqualValues(map[string]string{"_fk": "false", "_busy_timeout": "5000"}, cd.Options, "given url: sqlite3:///tmp/foo.db?_fk=false&foo=bar")
}

func Test_ConnectionDetails_Finalize_SQLite_SynURL_Only(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: N/A")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite:///tmp/foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: sqlite3:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_Dialect_URL(t *testing.T) {
	r := require.New(t)
	cd := &ConnectionDetails{
		Dialect: "sqlite3",
		URL:     "sqlite3:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: sqlite3")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite3:///tmp/foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: sqlite3:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_Dialect_SynURL(t *testing.T) {
	r := require.New(t)
	cd := &ConnectionDetails{
		Dialect: "sqlite3",
		URL:     "sqlite:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: sqlite3")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite:///tmp/foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: sqlite3:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_Synonym_URL(t *testing.T) {
	r := require.New(t)
	cd := &ConnectionDetails{
		Dialect: "sqlite",
		URL:     "sqlite3:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: sqlite")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite3:///tmp/foo.db")
	r.Equal(sqliteDefaultOptions, cd.Options, "given url: sqlite3:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_Synonym_SynURL(t *testing.T) {
	r := require.New(t)
	cd := &ConnectionDetails{
		Dialect: "sqlite",
		URL:     "sqlite:///tmp/foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: sqlite")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite:///tmp/foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: sqlite:///tmp/foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_Synonym_Path(t *testing.T) {
	r := require.New(t)
	cd := &ConnectionDetails{
		Dialect:  "sqlite",
		Database: "./foo.db",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: sqlite")
	r.Equal("./foo.db", cd.Database, "given database: ./foo.db")
	r.EqualValues(sqliteDefaultOptions, cd.Options, "given url: ./foo.db")
}

func Test_ConnectionDetails_Finalize_SQLite_OverrideOptions_Synonym_Path(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite3:///tmp/foo.db?_fk=false&foo=bar",
	}
	err := cd.Finalize() // calls withURL() and urlParserSQLite3(cd)
	r.NoError(err)
	r.Equal("sqlite3", cd.Dialect, "given dialect: N/A")
	r.Equal("/tmp/foo.db", cd.Database, "given url: sqlite3:///tmp/foo.db")
	r.EqualValues(map[string]string{"_fk": "false", "_busy_timeout": "5000"}, cd.Options, "given url: sqlite3:///tmp/foo.db?_fk=false&foo=bar")
}

func Test_ConnectionDetails_FinalizeOSPath(t *testing.T) {
	r := require.New(t)
	d := t.TempDir()
	p := filepath.Join(d, "testdb.sqlite")
	cd := &ConnectionDetails{
		Dialect:  "sqlite",
		Database: p,
	}
	r.NoError(cd.Finalize())
	r.Equal("sqlite3", cd.Dialect)
	r.EqualValues(p, cd.Database)
}

func Test_ConnectionDetails_Finalize_SQLite_NoTimeFormatDefault(t *testing.T) {
	t.Parallel()
	// finalizerSQLite must NOT add _time_format=sqlite as a default.
	//
	// _time_format=sqlite maps to the write format "2006-01-02 15:04:05.999999999-07:00"
	// (not timezone-free as the name implies). For a UTC time this produces
	// "2024-06-15 10:30:00+00:00", which time.Parse reads back as
	// FixedZone("", 0) — a broken, unnamed zero-offset zone distinct from time.UTC.
	//
	// Without any _time_format, modernc uses t.String() which includes the
	// timezone name ("... +0000 UTC"). Go's time.Parse recognises "UTC" as the
	// canonical time.UTC pointer, so no FixedZone workarounds are needed.
	for _, url := range []string{
		"sqlite3:///tmp/foo.db",
		"sqlite:///tmp/foo.db",
	} {
		t.Run(url, func(t *testing.T) {
			cd := &ConnectionDetails{URL: url}
			require.NoError(t, cd.Finalize())
			require.NotContains(t, cd.RawOptions, "_time_format",
				"finalizerSQLite must not inject _time_format — doing so would break UTC round-trips")
		})
	}
}

func TestSqlite_CreateDB(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{Dialect: "sqlite"}
	dialect, err := newSQLite(cd)
	r.NoError(err)

	t.Run("CreateFile", func(t *testing.T) {
		dir := t.TempDir()
		cd.Database = filepath.Join(dir, "testdb.sqlite")

		r.NoError(dialect.CreateDB())
		r.FileExists(cd.Database)
	})

	t.Run("MemoryDB_tag", func(t *testing.T) {
		dir := t.TempDir()
		cd.Database = filepath.Join(dir, "file::memory:?cache=shared")

		r.NoError(dialect.CreateDB())
		r.NoFileExists(cd.Database)
	})

	t.Run("MemoryDB_only", func(t *testing.T) {
		dir := t.TempDir()
		cd.Database = filepath.Join(dir, ":memory:")

		r.NoError(dialect.CreateDB())
		r.NoFileExists(cd.Database)
	})

	t.Run("MemoryDB_param", func(t *testing.T) {
		dir := t.TempDir()
		cd.Database = filepath.Join(dir, "file:foobar?mode=memory&cache=shared")

		r.NoError(dialect.CreateDB())
		r.NoFileExists(cd.Database)
	})

	t.Run("CreateFile_ExistingDB", func(t *testing.T) {
		dir := t.TempDir()
		cd.Database = filepath.Join(dir, "testdb.sqlite")

		r.NoError(dialect.CreateDB())
		r.EqualError(dialect.CreateDB(), fmt.Sprintf("could not create SQLite database '%s'; database exists", cd.Database))
	})

}

func TestSqlite_NewDriver(t *testing.T) {
	_, err := newSQLiteDriver()
	require.NoError(t, err)
}

func Test_normalizeTimesToUTC(t *testing.T) {
	fixedZone := time.FixedZone("", 0) // unnamed zero-offset zone, distinct from time.UTC
	local := time.Local

	now := time.Now().Truncate(time.Second)
	utcNow := now.UTC()
	fixedNow := now.In(fixedZone)
	localNow := now.In(local)

	t.Run("time.Time fields normalized", func(t *testing.T) {
		type row struct {
			T time.Time
		}
		for _, input := range []time.Time{fixedNow, localNow, utcNow} {
			r := &row{T: input}
			normalizeTimesToUTC(r)
			require.Equal(t, time.UTC, r.T.Location(), "Location must be time.UTC")
			require.True(t, utcNow.Equal(r.T), "instant must be preserved")
		}
	})

	t.Run("sql.NullTime valid normalized", func(t *testing.T) {
		type row struct {
			NT sql.NullTime
		}
		r := &row{NT: sql.NullTime{Time: fixedNow, Valid: true}}
		normalizeTimesToUTC(r)
		require.Equal(t, time.UTC, r.NT.Time.Location())
		require.True(t, utcNow.Equal(r.NT.Time))
		require.True(t, r.NT.Valid)
	})

	t.Run("sql.NullTime invalid untouched", func(t *testing.T) {
		type row struct {
			NT sql.NullTime
		}
		r := &row{NT: sql.NullTime{Valid: false}}
		normalizeTimesToUTC(r)
		require.False(t, r.NT.Valid)
		require.True(t, r.NT.Time.IsZero())
	})

	t.Run("embedded struct fields normalized", func(t *testing.T) {
		// Exported embedded types (e.g. pop.Model, pop.Timestamps) are the
		// real-world case; reflection's CanSet() returns false for unexported
		// embedded fields so they cannot be walked.
		type Inner struct{ CreatedAt time.Time }
		type outer struct {
			Inner
			UpdatedAt time.Time
		}
		r := &outer{
			Inner:     Inner{CreatedAt: fixedNow},
			UpdatedAt: localNow,
		}
		normalizeTimesToUTC(r)
		require.Equal(t, time.UTC, r.CreatedAt.Location())
		require.Equal(t, time.UTC, r.UpdatedAt.Location())
	})

	t.Run("slice of structs normalized", func(t *testing.T) {
		type row struct{ T time.Time }
		rows := []row{{fixedNow}, {localNow}, {utcNow}}
		normalizeTimesToUTC(&rows)
		for _, r := range rows {
			require.Equal(t, time.UTC, r.T.Location())
			require.True(t, utcNow.Equal(r.T))
		}
	})

	t.Run("slice of pointer-to-struct normalized", func(t *testing.T) {
		type row struct{ T time.Time }
		rows := []*row{{fixedNow}, {localNow}}
		normalizeTimesToUTC(&rows)
		for _, r := range rows {
			require.Equal(t, time.UTC, r.T.Location())
		}
	})

	t.Run("already UTC unchanged", func(t *testing.T) {
		type row struct{ T time.Time }
		r := &row{T: utcNow}
		normalizeTimesToUTC(r)
		require.Equal(t, time.UTC, r.T.Location())
		require.True(t, utcNow.Equal(r.T))
	})

	t.Run("nil pointer no panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			normalizeTimesToUTC((*struct{ T time.Time })(nil))
		})
	})

	t.Run("*time.Time field normalized", func(t *testing.T) {
		type row struct{ T *time.Time }
		r := &row{T: &fixedNow}
		normalizeTimesToUTC(r)
		require.NotNil(t, r.T)
		require.Equal(t, time.UTC, r.T.Location())
		require.True(t, utcNow.Equal(*r.T))
	})

	t.Run("nil *time.Time field not panicking", func(t *testing.T) {
		type row struct{ T *time.Time }
		r := &row{T: nil}
		require.NotPanics(t, func() { normalizeTimesToUTC(r) })
		require.Nil(t, r.T)
	})

	t.Run("pointer-to-struct with time fields", func(t *testing.T) {
		type Inner struct{ CreatedAt time.Time }
		type outer struct{ Details *Inner }
		r := &outer{Details: &Inner{CreatedAt: fixedNow}}
		normalizeTimesToUTC(r)
		require.Equal(t, time.UTC, r.Details.CreatedAt.Location())
		require.True(t, utcNow.Equal(r.Details.CreatedAt))
	})
}

// parsePragmas returns the _pragma slice from cd.RawOptions.
func parsePragmas(t *testing.T, cd *ConnectionDetails) []string {
	t.Helper()
	q, err := url.ParseQuery(cd.RawOptions)
	require.NoError(t, err)
	return q["_pragma"]
}

// Test_sqlitePragmaSet verifies that the pragma name must be followed by '('
// to avoid false-positive prefix matches (e.g. foreign_keys_per_table).
func Test_sqlitePragmaSet(t *testing.T) {
	tests := []struct {
		name       string
		pragma     string
		values     []string
		wantResult bool
	}{
		{"exact match", "foreign_keys", []string{"foreign_keys(1)"}, true},
		{"case insensitive", "foreign_keys", []string{"FOREIGN_KEYS(1)"}, true},
		{"leading whitespace", "foreign_keys", []string{" foreign_keys(1)"}, true},
		{"does NOT match longer name", "foreign_keys", []string{"foreign_keys_per_table(1)"}, false},
		{"match in multi-value slice", "busy_timeout", []string{"foreign_keys(1)", "busy_timeout(5000)"}, true},
		{"empty values", "foreign_keys", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{"_pragma": tc.values}
			require.Equal(t, tc.wantResult, sqlitePragmaSet(q, tc.pragma))
		})
	}
}

// Test_ConnectionDetails_Finalize_SQLite_RawOptions_Defaults asserts that
// finalizerSQLite injects defaults as _pragma entries and echoes them back.
func Test_ConnectionDetails_Finalize_SQLite_RawOptions_Defaults(t *testing.T) {
	cd := &ConnectionDetails{URL: "sqlite3:///tmp/foo.db"}
	require.NoError(t, cd.Finalize())

	pragmas := parsePragmas(t, cd)
	require.Contains(t, pragmas, "busy_timeout(5000)")
	require.Contains(t, pragmas, "foreign_keys(1)")

	q, _ := url.ParseQuery(cd.RawOptions)
	require.NotContains(t, q, "_fk")
	require.NotContains(t, q, "_busy_timeout")

	require.Equal(t, "1", cd.Options["_fk"])
	require.Equal(t, "5000", cd.Options["_busy_timeout"])
}

// Test_ConnectionDetails_Finalize_SQLite_RawOptions_Override asserts that
// explicit legacy params translate correctly and unsupported params are stripped.
func Test_ConnectionDetails_Finalize_SQLite_RawOptions_Override(t *testing.T) {
	cd := &ConnectionDetails{URL: "sqlite3:///tmp/foo.db?_fk=false&foo=bar"}
	require.NoError(t, cd.Finalize())

	pragmas := parsePragmas(t, cd)
	require.Contains(t, pragmas, "foreign_keys(false)")
	require.NotContains(t, pragmas, "foreign_keys(true)")
	require.Contains(t, pragmas, "busy_timeout(5000)")

	q, _ := url.ParseQuery(cd.RawOptions)
	require.NotContains(t, q, "foo", "unsupported params must be stripped")
	require.NotContains(t, q, "_fk")

	require.Equal(t, "false", cd.Options["_fk"])
}

// Test_ConnectionDetails_Finalize_SQLite_LegacyParams covers the full set of
// mattn-compatible legacy params translated to _pragma entries.
func Test_ConnectionDetails_Finalize_SQLite_LegacyParams(t *testing.T) {
	tests := []struct {
		name   string
		param  string
		value  string
		pragma string
	}{
		{"foreign_keys long form", "_foreign_keys", "0", "foreign_keys(0)"},
		{"foreign_keys alias", "_fk", "0", "foreign_keys(0)"},
		{"journal_mode", "_journal_mode", "WAL", "journal_mode(WAL)"},
		{"journal_mode alias", "_journal", "WAL", "journal_mode(WAL)"},
		{"busy_timeout", "_busy_timeout", "1000", "busy_timeout(1000)"},
		{"busy_timeout alias", "_timeout", "1000", "busy_timeout(1000)"},
		{"synchronous", "_synchronous", "NORMAL", "synchronous(NORMAL)"},
		{"synchronous alias", "_sync", "NORMAL", "synchronous(NORMAL)"},
		{"auto_vacuum", "_auto_vacuum", "FULL", "auto_vacuum(FULL)"},
		{"auto_vacuum alias", "_vacuum", "FULL", "auto_vacuum(FULL)"},
		{"locking_mode", "_locking_mode", "EXCLUSIVE", "locking_mode(EXCLUSIVE)"},
		{"secure_delete", "_secure_delete", "true", "secure_delete(true)"},
		{"recursive_triggers", "_recursive_triggers", "true", "recursive_triggers(true)"},
		{"query_only", "_query_only", "true", "query_only(true)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cd := &ConnectionDetails{
				Dialect:  "sqlite",
				Database: "./foo.db",
				Options:  map[string]string{tc.param: tc.value},
			}
			require.NoError(t, cd.Finalize())

			pragmas := parsePragmas(t, cd)
			require.Contains(t, pragmas, tc.pragma)

			q, _ := url.ParseQuery(cd.RawOptions)
			require.NotContains(t, q, tc.param, "legacy param must be translated away from DSN")
		})
	}
}

// Test_ConnectionDetails_Finalize_SQLite_Programmatic tests the path where
// Options is set directly (no URL), verifying DSN and echo-back.
func Test_ConnectionDetails_Finalize_SQLite_Programmatic(t *testing.T) {
	cd := &ConnectionDetails{
		Dialect:  "sqlite",
		Database: "./foo.db",
		Options: map[string]string{
			"_journal_mode": "WAL",
			"_busy_timeout": "10000",
		},
	}
	require.NoError(t, cd.Finalize())

	pragmas := parsePragmas(t, cd)
	require.Contains(t, pragmas, "journal_mode(WAL)")
	require.Contains(t, pragmas, "busy_timeout(10000)")
	require.Contains(t, pragmas, "foreign_keys(1)")

	q, _ := url.ParseQuery(cd.RawOptions)
	require.NotContains(t, q, "_journal_mode")
	require.NotContains(t, q, "_busy_timeout")
	require.NotContains(t, q, "_fk")

	require.Equal(t, "WAL", cd.Options["_journal_mode"])
	require.Equal(t, "10000", cd.Options["_busy_timeout"])
	require.Equal(t, "1", cd.Options["_fk"])
}

// Test_ConnectionDetails_Finalize_SQLite_DirectPragma verifies that
// _pragma=name(value) entries set directly in the DSN survive and are echoed.
func Test_ConnectionDetails_Finalize_SQLite_DirectPragma(t *testing.T) {
	cd := &ConnectionDetails{
		URL: "sqlite3:///tmp/foo.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)",
	}
	require.NoError(t, cd.Finalize())

	pragmas := parsePragmas(t, cd)
	require.Contains(t, pragmas, "journal_mode(WAL)")
	require.Contains(t, pragmas, "foreign_keys(1)")
	require.NotContains(t, pragmas, "foreign_keys(true)") // default not added again

	require.Equal(t, "WAL", cd.Options["_journal_mode"])
	require.Equal(t, "1", cd.Options["_fk"])
}
