package pop

import (
	"io"
	"testing"

	"github.com/gobuffalo/fizz"
	"github.com/ory/pop/v6/columns"
	"github.com/stretchr/testify/require"
)

// fakeDialect is a minimal Dialect implementation used to exercise custom
// dialect registration. It embeds commonDialect for Lock and Quote and stubs
// the remaining methods; the connection paths it implements are never executed
// by these tests.
type fakeDialect struct {
	commonDialect
	name string
}

var _ dialect = (*fakeDialect)(nil)

func (d *fakeDialect) Name() string                                      { return d.name }
func (d *fakeDialect) DefaultDriver() string                             { return d.name }
func (d *fakeDialect) URL() string                                       { return d.ConnectionDetails.URL }
func (d *fakeDialect) MigrationURL() string                              { return d.ConnectionDetails.URL }
func (d *fakeDialect) Details() *ConnectionDetails                       { return d.ConnectionDetails }
func (d *fakeDialect) TranslateSQL(sql string) string                    { return sql }
func (d *fakeDialect) CreateDB() error                                   { return nil }
func (d *fakeDialect) DropDB() error                                     { return nil }
func (d *fakeDialect) DumpSchema(io.Writer) error                        { return nil }
func (d *fakeDialect) LoadSchema(io.Reader) error                        { return nil }
func (d *fakeDialect) TruncateAll(*Connection) error                     { return nil }
func (d *fakeDialect) FizzTranslator() fizz.Translator                   { return nil }
func (d *fakeDialect) SelectOne(*Connection, *Model, Query) error        { return nil }
func (d *fakeDialect) SelectMany(*Connection, *Model, Query) error       { return nil }
func (d *fakeDialect) Create(*Connection, *Model, columns.Columns) error { return nil }
func (d *fakeDialect) Update(*Connection, *Model, columns.Columns) error { return nil }
func (d *fakeDialect) UpdateQuery(*Connection, *Model, columns.Columns, Query) (int64, error) {
	return 0, nil
}
func (d *fakeDialect) Destroy(*Connection, *Model) error       { return nil }
func (d *fakeDialect) Delete(*Connection, *Model, Query) error { return nil }

func Test_RegisterDialect(t *testing.T) {
	const (
		name = "poptestduck"
		syn  = "poptestquack"
	)

	var (
		newConnCalled bool
		urlParsed     bool
		finalized     bool
	)

	err := RegisterDialect(DialectRegistration{
		Name:     name,
		Synonyms: []string{syn},
		NewConnection: func(cd *ConnectionDetails) (Dialect, error) {
			newConnCalled = true
			return &fakeDialect{commonDialect: commonDialect{ConnectionDetails: cd}, name: name}, nil
		},
		URLParser: func(cd *ConnectionDetails) error {
			urlParsed = true
			cd.Database = "duckdb"
			return nil
		},
		Finalizer: func(cd *ConnectionDetails) {
			finalized = true
		},
	})
	require.NoError(t, err)

	t.Run("supported and canonicalized", func(t *testing.T) {
		require.True(t, DialectSupported(name))
		require.Equal(t, name, CanonicalDialect(syn))
		require.Equal(t, name, CanonicalDialect("POPTESTQUACK"))
	})

	t.Run("dispatches to custom NewConnection with parser and finalizer", func(t *testing.T) {
		c, err := NewConnection(&ConnectionDetails{URL: name + "://mydb"})
		require.NoError(t, err)
		require.True(t, newConnCalled)
		require.True(t, urlParsed)
		require.True(t, finalized)
		require.Equal(t, name, c.Dialect.Name())
		require.Equal(t, "duckdb", c.Dialect.Details().Database)
	})

	t.Run("rejects duplicate name", func(t *testing.T) {
		err := RegisterDialect(DialectRegistration{
			Name:          name,
			NewConnection: func(*ConnectionDetails) (Dialect, error) { return nil, nil },
		})
		require.Error(t, err)
	})

	t.Run("rejects duplicate synonym", func(t *testing.T) {
		err := RegisterDialect(DialectRegistration{
			Name:          "poptestgoose",
			Synonyms:      []string{syn},
			NewConnection: func(*ConnectionDetails) (Dialect, error) { return nil, nil },
		})
		require.Error(t, err)
	})

	t.Run("rejects missing name", func(t *testing.T) {
		err := RegisterDialect(DialectRegistration{
			NewConnection: func(*ConnectionDetails) (Dialect, error) { return nil, nil },
		})
		require.Error(t, err)
	})

	t.Run("rejects missing NewConnection", func(t *testing.T) {
		err := RegisterDialect(DialectRegistration{Name: "poptestheron"})
		require.Error(t, err)
	})
}
