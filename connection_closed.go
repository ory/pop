package pop

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"slices"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsConnectionClosed reports whether err means the connection carrying the
// statement went away. A read that fails this way did not produce a complete
// result, so a caller may safely run it again on a new connection. A write may
// not: the server may have applied it before the connection dropped.
func IsConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	if isContextError(err) {
		return false
	}
	if containsSQLState(err, "08007", "40003") {
		return false
	}
	switch SQLState(err) {
	case "08000", // connection_exception
		"08001", // sqlclient_unable_to_establish_sqlconnection
		"08003", // connection_does_not_exist
		"08004", // sqlserver_rejected_establishment_of_sqlconnection
		"08006", // connection_failure
		"57P01", // admin_shutdown
		"57P02", // crash_shutdown
		"57P03": // cannot_connect_now
		return true
	}
	// io.ErrUnexpectedEOF is the loosest of these: it also covers a message
	// truncated for reasons other than the connection closing. Retrying a read
	// on it costs one bounded round of attempts and returns the same error.
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, pgconn.ErrConnClosed)
}

func containsSQLState(err error, states ...string) bool {
	if err == nil {
		return false
	}
	if state, ok := err.(interface{ SQLState() string }); ok && slices.Contains(states, state.SQLState()) {
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		return slices.ContainsFunc(wrapped.Unwrap(), func(child error) bool {
			return containsSQLState(child, states...)
		})
	case interface{ Unwrap() error }:
		return containsSQLState(wrapped.Unwrap(), states...)
	}
	return false
}

// sqlStateError is an error that reports a SQLSTATE, as pgconn and lib/pq do.
type sqlStateError interface {
	error
	SQLState() string
}

// SQLState returns the SQLSTATE an error reports, or "" when it reports none.
func SQLState(err error) string {
	if state, ok := errors.AsType[sqlStateError](err); ok {
		return state.SQLState()
	}
	return ""
}
