package pop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestIsConnectionClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nil":                                {nil, false},
		"admin shutdown":                     {&pgconn.PgError{Code: "57P01"}, true},
		"crash shutdown":                     {&pgconn.PgError{Code: "57P02"}, true},
		"cannot connect now":                 {&pgconn.PgError{Code: "57P03"}, true},
		"connection exception":               {&pgconn.PgError{Code: "08006"}, true},
		"connection failure":                 {&pgconn.PgError{Code: "08000"}, true},
		"client cannot establish connection": {&pgconn.PgError{Code: "08001"}, true},
		"connection does not exist":          {&pgconn.PgError{Code: "08003"}, true},
		"server rejected connection":         {&pgconn.PgError{Code: "08004"}, true},
		"transaction resolution unknown":     {&pgconn.PgError{Code: "08007"}, false},
		"serialization failure":              {&pgconn.PgError{Code: "40001"}, false},
		"statement completion unknown":       {&pgconn.PgError{Code: "40003"}, false},
		"unique violation":                   {&pgconn.PgError{Code: "23505"}, false},
		"bad conn":                           {driver.ErrBadConn, true},
		"net closed":                         {net.ErrClosed, true},
		"unexpected eof":                     {io.ErrUnexpectedEOF, true},
		"sql connection done":                {sql.ErrConnDone, true},
		"connection reset":                   {syscall.ECONNRESET, true},
		"connection aborted":                 {syscall.ECONNABORTED, true},
		"broken pipe":                        {syscall.EPIPE, true},
		"pgconn closed":                      {pgconn.ErrConnClosed, true},
		"wrapped":                            {fmt.Errorf("select: %w", &pgconn.PgError{Code: "57P01"}), true},
		"unrelated":                          {errors.New("boom"), false},
		"cancelled retry":                    {errors.Join(context.Canceled, &pgconn.PgError{Code: "57P01"}), false},
		"expired retry":                      {errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF), false},
		// An unknown outcome stays unknown even when the chain also carries a
		// connection sentinel: the statement may have been applied.
		"transaction resolution unknown behind a network error": {
			fmt.Errorf("receive: %w: %w", io.ErrUnexpectedEOF, &pgconn.PgError{Code: "08007"}), false,
		},
		"statement completion unknown behind a closed connection": {
			fmt.Errorf("receive: %w: %w", net.ErrClosed, &pgconn.PgError{Code: "40003"}), false,
		},
		"transaction resolution unknown after a retryable SQLSTATE": {
			errors.Join(&pgconn.PgError{Code: "08006"}, fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "08007"})), false,
		},
		"statement completion unknown before a retryable SQLSTATE": {
			errors.Join(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40003"}), &pgconn.PgError{Code: "08006"}), false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsConnectionClosed(tc.err))
		})
	}
}

func TestSQLState(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"nil":       {nil, ""},
		"pg error":  {&pgconn.PgError{Code: "57P01"}, "57P01"},
		"wrapped":   {fmt.Errorf("select: %w", &pgconn.PgError{Code: "08006"}), "08006"},
		"unrelated": {errors.New("boom"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, SQLState(tc.err))
		})
	}
}
