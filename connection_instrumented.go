package pop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"sync"

	mysqld "github.com/go-sql-driver/mysql"
	"github.com/gobuffalo/pop/v6/logging"
	pgx "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/luna-duclos/instrumentedsql"
)

const instrumentedDriverName = "instrumented-sql-driver"

var sqlDriverLock = sync.Mutex{}

func instrumentDriver(deets *ConnectionDetails, defaultDriverName string) (driverName, dialect string, err error) {
	driverName = defaultDriverName
	if deets.Driver != "" {
		driverName = deets.Driver
	}
	dialect = driverName

	if !deets.UseInstrumentedDriver {
		if len(deets.InstrumentedDriverOptions) > 0 {
			log(logging.Warn, "SQL driver instrumentation is disabled but `ConnectionDetails.InstrumentedDriverOptions` is not empty. Please double-check if this is a error.")
		}

		// If instrumentation is disabled, we just return the driver name we got (e.g. "pgx").
		return driverName, dialect, nil
	}

	if len(deets.InstrumentedDriverOptions) == 0 {
		log(logging.Warn, "SQL driver instrumentation was enabled but no options have been passed to `ConnectionDetails.InstrumentedDriverOptions`. Instrumentation will therefore not result in any output.")
	}

	var dr driver.Driver
	var newDriverName string
	switch CanonicalDialect(driverName) {
	case nameCockroach:
		fallthrough
	case namePostgreSQL:
		dr = new(pgx.Driver)
		newDriverName = instrumentedDriverName + "-" + namePostgreSQL
	case nameMariaDB:
		fallthrough
	case nameMySQL:
		dr = mysqld.MySQLDriver{}
		newDriverName = instrumentedDriverName + "-" + nameMySQL
	case nameSQLite3:
		var err error
		dr, err = newSQLiteDriver()
		if err != nil {
			return "", "", err
		}
		newDriverName = instrumentedDriverName + "-" + nameSQLite3
	}

	sqlDriverLock.Lock()
	defer sqlDriverLock.Unlock()

	var found bool
	for _, n := range sql.Drivers() {
		if n == newDriverName {
			found = true
			break
		}
	}

	if !found {
		sql.Register(newDriverName, instrumentedsql.WrapDriver(dr, deets.InstrumentedDriverOptions...))
	}

	return newDriverName, dialect, nil
}

// openPotentiallyInstrumentedConnection first opens a raw SQL connection and then wraps it with `sqlx`.
//
// We do this because `sqlx` needs the database type in order to properly
// translate arguments (e.g. `?` to `$1`) in SQL queries. Because we use
// a custom driver name when using instrumentation, this detection would fail
// otherwise.
func openPotentiallyInstrumentedConnection(ctx context.Context, c dialect, dsn string) (*sqlx.DB, error) {
	driverName, dialect, err := instrumentDriver(c.Details(), c.DefaultDriver())
	if err != nil {
		return nil, err
	}

	switch CanonicalDialect(c.DefaultDriver()) {
	case nameCockroach:
		fallthrough
	case namePostgreSQL:
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, err
		}

		db := stdlib.OpenDBFromPool(pool)
		// GetPoolConnector creates a new driver.Connector from the given *pgxpool.Pool. By using this be sure to set the
		// maximum idle connections of the *sql.DB created with this connector to zero since they must be managed from the
		// *pgxpool.Pool. This is required to avoid acquiring all the connections from the pgxpool and starving any direct
		// users of the pgxpool.
		//
		// https://github.com/jackc/pgx/blob/c2175fe46e3d6f43af14a21b47386739d15e4ee0/stdlib/sql.go#L194-L197
		db.SetMaxIdleConns(0)

		/**
		// Create a pgx pool
		pool, err := pgxpool.New(context.Background(), "postgres://user:pass@localhost:5432/dbname")
		if err != nil {
		    log.Fatalf("Failed to create pgx pool: %v", err)
		}

		// Define metrics
		var (
		    totalConns = prometheus.NewGaugeFunc(
		        prometheus.GaugeOpts{
		            Name: "pgx_pool_total_connections",
		            Help: "Number of total connections in the pgx pool",
		        },
		        func() float64 {
		            stats := pool.Stat()
		            return float64(stats.TotalConns())
		        },
		    )
		    idleConns = prometheus.NewGaugeFunc(
		        prometheus.GaugeOpts{
		            Name: "pgx_pool_idle_connections",
		            Help: "Number of idle connections in the pgx pool",
		        },
		        func() float64 {
		            stats := pool.Stat()
		            return float64(stats.IdleConns())
		        },
		    )
		    busyConns = prometheus.NewGaugeFunc(
		        prometheus.GaugeOpts{
		            Name: "pgx_pool_busy_connections",
		            Help: "Number of busy connections in the pgx pool",
		        },
		        func() float64 {
		            stats := pool.Stat()
		            return float64(stats.TotalConns() - stats.IdleConns())
		        },
		    )
		)

		*/

		return sqlx.NewDb(db, dialect), nil
	}

	con, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open database connection: %w", err)
	}

	details := c.Details()
	if details.Pool != 0 {
		con.SetMaxOpenConns(details.Pool)
	}
	if details.IdlePool != 0 {
		con.SetMaxIdleConns(details.IdlePool)
	}
	if details.ConnMaxLifetime > 0 {
		con.SetConnMaxLifetime(details.ConnMaxLifetime)
	}
	if details.ConnMaxIdleTime > 0 {
		con.SetConnMaxIdleTime(details.ConnMaxIdleTime)
	}

	return sqlx.NewDb(con, dialect), nil
}
