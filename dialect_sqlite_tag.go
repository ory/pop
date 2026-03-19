package pop

import (
	"database/sql"

	moderncsqlite "modernc.org/sqlite" // Load SQLite3 pure-Go driver
)

func init() {
	sql.Register("sqlite3", &moderncsqlite.Driver{})
}
