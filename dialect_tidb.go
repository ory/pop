package pop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	_mysql "github.com/go-sql-driver/mysql" // Load MySQL Go driver
	"github.com/gobuffalo/fizz"
	"github.com/gobuffalo/fizz/translators"
	"github.com/jmoiron/sqlx"
	"github.com/ory/pop/v6/columns"
	"github.com/ory/pop/v6/internal/defaults"
	"github.com/ory/pop/v6/logging"
)

const nameTiDB = "tidb"
const hostTiDB = "127.0.0.1"
const portTiDB = "4000"

func init() {
	AvailableDialects = append(AvailableDialects, nameTiDB)
	urlParser[nameTiDB] = urlParserTiDB
	finalizer[nameTiDB] = finalizerTiDB
	newConnection[nameTiDB] = newTiDB
}

var _ dialect = &tidb{}

type tidb struct {
	commonDialect
}

func (m *tidb) Name() string {
	return nameTiDB
}

func (m *tidb) DefaultDriver() string {
	return nameMySQL
}

func (tidb) Quote(key string) string {
	return fmt.Sprintf("`%s`", key)
}

func (m *tidb) Details() *ConnectionDetails {
	return m.ConnectionDetails
}

func (m *tidb) URL() string {
	cd := m.ConnectionDetails
	if cd.URL != "" {
		url := strings.TrimPrefix(cd.URL, "tidb://")
		url = strings.TrimPrefix(url, "mysql://")
		return url
	}

	user := fmt.Sprintf("%s:%s@", cd.User, cd.Password)
	user = strings.Replace(user, ":@", "@", 1)
	if user == "@" || strings.HasPrefix(user, ":") {
		user = ""
	}

	addr := fmt.Sprintf("(%s:%s)", cd.Host, cd.Port)
	// in case of unix domain socket, tricky.
	// it is better to check Host is not valid inet address or has '/'.
	if cd.Port == "socket" {
		addr = fmt.Sprintf("unix(%s)", cd.Host)
	}

	s := "%s%s/%s?%s"
	return fmt.Sprintf(s, user, addr, cd.Database, cd.OptionsString(""))
}

func (m *tidb) urlWithoutDB() string {
	cd := m.ConnectionDetails
	return strings.Replace(m.URL(), "/"+cd.Database+"?", "/?", 1)
}

func (m *tidb) MigrationURL() string {
	return m.URL()
}

func (m *tidb) Create(c *Connection, model *Model, cols columns.Columns) error {
	if err := genericCreate(c, model, cols, m); err != nil {
		return fmt.Errorf("tidb create: %w", err)
	}
	return nil
}

func (m *tidb) Update(c *Connection, model *Model, cols columns.Columns) error {
	if err := genericUpdate(c, model, cols, m); err != nil {
		return fmt.Errorf("tidb update: %w", err)
	}
	return nil
}

func (m *tidb) UpdateQuery(c *Connection, model *Model, cols columns.Columns, query Query) (int64, error) {
	if n, err := genericUpdateQuery(c, model, cols, m, query, sqlx.QUESTION); err != nil {
		return n, fmt.Errorf("tidb update query: %w", err)
	} else {
		return n, nil
	}
}

func (m *tidb) Destroy(c *Connection, model *Model) error {
	stmt := fmt.Sprintf("DELETE FROM %s  WHERE %s = ?", m.Quote(model.TableName()), model.IDField())
	_, err := genericExec(c, stmt, model.ID())
	if err != nil {
		return fmt.Errorf("tidb destroy: %w", err)
	}
	return nil
}

func (m *tidb) Delete(c *Connection, model *Model, query Query) error {
	sqlQuery, args := query.ToSQL(model)
	// * MySQL does not support table alias for DELETE syntax until 8.0.
	// * Do not generate SQL manually if they may have `WHERE IN`.
	// * Spaces are intentionally added to make it easy to see on the log.
	sqlQuery = asRegex.ReplaceAllString(sqlQuery, "  ")

	_, err := genericExec(c, sqlQuery, args...)
	return err
}

func (m *tidb) SelectOne(c *Connection, model *Model, query Query) error {
	if err := genericSelectOne(c, model, query); err != nil {
		return fmt.Errorf("tidb select one: %w", err)
	}
	return nil
}

func (m *tidb) SelectMany(c *Connection, models *Model, query Query) error {
	if err := genericSelectMany(c, models, query); err != nil {
		return fmt.Errorf("tidb select many: %w", err)
	}
	return nil
}

// CreateDB creates a new database, from the given connection credentials
func (m *tidb) CreateDB() error {
	deets := m.ConnectionDetails
	db, _, err := openPotentiallyInstrumentedConnection(context.Background(), m, m.urlWithoutDB())
	if err != nil {
		return fmt.Errorf("error creating TiDB database %s: %w", deets.Database, err)
	}
	defer db.Close()
	charset := defaults.String(deets.option("charset"), "utf8mb4")
	encoding := defaults.String(deets.option("collation"), "utf8mb4_general_ci")
	query := fmt.Sprintf("CREATE DATABASE `%s` DEFAULT CHARSET `%s` DEFAULT COLLATE `%s`", deets.Database, charset, encoding)
	log(logging.SQL, query)

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("error creating TiDB database %s: %w", deets.Database, err)
	}

	log(logging.Info, "created database %s", deets.Database)
	return nil
}

// DropDB drops an existing database, from the given connection credentials
func (m *tidb) DropDB() error {
	deets := m.ConnectionDetails
	db, _, err := openPotentiallyInstrumentedConnection(context.Background(), m, m.urlWithoutDB())
	if err != nil {
		return fmt.Errorf("error dropping TiDB database %s: %w", deets.Database, err)
	}
	defer db.Close()
	query := fmt.Sprintf("DROP DATABASE `%s`", deets.Database)
	log(logging.SQL, query)

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("error dropping TiDB database %s: %w", deets.Database, err)
	}

	log(logging.Info, "dropped database %s", deets.Database)
	return nil
}

func (m *tidb) TranslateSQL(sql string) string {
	return sql
}

func (m *tidb) FizzTranslator() fizz.Translator {
	t := translators.NewMySQL(m.URL(), m.Details().Database)
	return t
}

func (m *tidb) DumpSchema(w io.Writer) error {
	deets := m.Details()
	cmd := exec.Command("mysqldump", "--protocol", "TCP", "-d", "-h", deets.Host, "-P", deets.Port, "-u", deets.User, fmt.Sprintf("--password=%s", deets.Password), deets.Database)
	if deets.Port == "socket" {
		cmd = exec.Command("mysqldump", "-d", "-S", deets.Host, "-u", deets.User, fmt.Sprintf("--password=%s", deets.Password), deets.Database)
	}
	return genericDumpSchema(deets, cmd, w)
}

// LoadSchema executes a schema sql file against the configured database.
func (m *tidb) LoadSchema(r io.Reader) error {
	return genericLoadSchema(m, r)
}

// TruncateAll truncates all tables for the given connection.
func (m *tidb) TruncateAll(tx *Connection) error {
	var stmts []string
	err := tx.RawQuery(tidbTruncate, m.Details().Database, tx.MigrationTableName()).All(&stmts)
	if err != nil {
		return err
	}
	if len(stmts) == 0 {
		return nil
	}

	var qb bytes.Buffer
	// #49: Disable foreign keys before truncation
	qb.WriteString("SET SESSION FOREIGN_KEY_CHECKS = 0; ")
	qb.WriteString(strings.Join(stmts, " "))
	// #49: Re-enable foreign keys after truncation
	qb.WriteString(" SET SESSION FOREIGN_KEY_CHECKS = 1;")

	return tx.RawQuery(qb.String()).Exec()
}

func (m *tidb) AfterOpen(c *Connection) error {
	// ref: ory/kratos#1551
	err := c.RawQuery("SET SESSION transaction_isolation = 'REPEATABLE-READ';").Exec()
	if err != nil {
		return fmt.Errorf("tidb: setting transaction isolation level: %w", err)
	}
	return nil
}

func newTiDB(deets *ConnectionDetails) (dialect, error) {
	cd := &tidb{
		commonDialect: commonDialect{ConnectionDetails: deets},
	}
	return cd, nil
}

func urlParserTiDB(cd *ConnectionDetails) error {
	dsn := cd.URL
	dsn = strings.TrimPrefix(dsn, "tidb://")
	dsn = strings.TrimPrefix(dsn, "mysql://")
	cfg, err := _mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("the URL '%s' is not supported by MySQL/TiDB driver: %w", cd.URL, err)
	}

	cd.User = cfg.User
	cd.Password = cfg.Passwd
	cd.Database = cfg.DBName

	// NOTE: use cfg.Params if want to fill options with full parameters
	cd.setOption("collation", cfg.Collation)

	if cfg.Net == "unix" {
		cd.Port = "socket" // trick. see: `URL()`
		cd.Host = cfg.Addr
	} else {
		tmp := strings.Split(cfg.Addr, ":")
		cd.Host = tmp[0]
		if len(tmp) > 1 {
			cd.Port = tmp[1]
		}
	}

	return nil
}

func finalizerTiDB(cd *ConnectionDetails) {
	cd.Host = defaults.String(cd.Host, hostTiDB)
	cd.Port = defaults.String(cd.Port, portTiDB)

	defs := map[string]string{
		"readTimeout": "3s",
		"collation":   "utf8mb4_general_ci",
	}
	forced := map[string]string{
		"parseTime":       "true",
		"multiStatements": "true",
	}

	for k, def := range defs {
		cd.setOptionWithDefault(k, cd.option(k), def)
	}

	for k, v := range forced {
		// respect user specified options but print warning!
		cd.setOptionWithDefault(k, cd.option(k), v)
		if cd.option(k) != v { // when user-defined option exists
			log(logging.Warn, "IMPORTANT! '%s: %s' option is required to work properly but your current setting is '%v: %v'.", k, v, k, cd.option(k))
			log(logging.Warn, "It is highly recommended to remove '%v: %v' option from your config!", k, cd.option(k))
		} // or override with `cd.Options[k] = v`?
		if cd.URL != "" && !strings.Contains(cd.URL, k+"="+v) {
			log(logging.Warn, "IMPORTANT! '%s=%s' option is required to work properly. Please add it to the database URL in the config!", k, v)
		} // or fix user specified url?
	}
}

const tidbTruncate = "SELECT concat('TRUNCATE TABLE `', TABLE_NAME, '`;') as stmt FROM INFORMATION_SCHEMA.TABLES WHERE table_schema = ? AND table_name <> ? AND table_type <> 'VIEW'"
