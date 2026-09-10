package pop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type countingStore struct {
	store
	failures int
	err      error
	calls    map[string]int
	onCall   func()
}

func newCountingStore(failures int, err error) *countingStore {
	return &countingStore{failures: failures, err: err, calls: map[string]int{}}
}

func (s *countingStore) record(name string) error {
	s.calls[name]++
	if s.onCall != nil {
		s.onCall()
	}
	if s.calls[name] <= s.failures {
		return s.err
	}
	return nil
}

func (s *countingStore) Get(interface{}, string, ...interface{}) error {
	return s.record("Get")
}

func (s *countingStore) Select(interface{}, string, ...interface{}) error {
	return s.record("Select")
}

func (s *countingStore) GetContext(context.Context, interface{}, string, ...interface{}) error {
	return s.record("GetContext")
}

func (s *countingStore) SelectContext(context.Context, interface{}, string, ...interface{}) error {
	return s.record("SelectContext")
}

func (s *countingStore) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, s.record("ExecContext")
}

func (s *countingStore) NamedExecContext(context.Context, string, interface{}) (sql.Result, error) {
	return nil, s.record("NamedExecContext")
}

func closedErr() error { return &pgconn.PgError{Code: "57P01"} }

type retryQueryModel struct {
	ID int `db:"id"`
}

type deadlineErrorContext struct {
	context.Context
}

func (c deadlineErrorContext) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func newRetryQueryTestConnection(t *testing.T, s store) *Connection {
	t.Helper()
	d, err := newPostgreSQL(&ConnectionDetails{Dialect: namePostgreSQL})
	require.NoError(t, err)
	return &Connection{Store: s, Dialect: d}
}

func TestGeneratedReadsRetryClosedConnections(t *testing.T) {
	for name, tc := range map[string]struct {
		method string
		run    func(*Connection) error
	}{
		"first": {
			method: "GetContext",
			run: func(c *Connection) error {
				return c.Where("id = ?", 1).First(&retryQueryModel{})
			},
		},
		"last": {
			method: "GetContext",
			run: func(c *Connection) error {
				return c.Where("id = ?", 1).Last(&retryQueryModel{})
			},
		},
		"all": {
			method: "SelectContext",
			run: func(c *Connection) error {
				return c.Where("id = ?", 1).All(&[]retryQueryModel{})
			},
		},
		"exists": {
			method: "Get",
			run: func(c *Connection) error {
				_, err := c.Where("id = ?", 1).Exists(&retryQueryModel{})
				return err
			},
		},
		"count": {
			method: "Get",
			run: func(c *Connection) error {
				_, err := c.Where("id = ?", 1).Count(&retryQueryModel{})
				return err
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := newCountingStore(2, closedErr())
			require.NoError(t, tc.run(newRetryQueryTestConnection(t, inner)))
			assert.Equal(t, 3, inner.calls[tc.method])
		})
	}
}

func TestRawQueriesAreOneShotByDefault(t *testing.T) {
	for name, tc := range map[string]struct {
		method    string
		statement string
		run       func(*Query) error
	}{
		"select": {
			method:    "GetContext",
			statement: "SELECT id FROM retry_query_models",
			run: func(q *Query) error {
				return q.First(&retryQueryModel{})
			},
		},
		"insert returning": {
			method:    "GetContext",
			statement: "INSERT INTO retry_query_models DEFAULT VALUES RETURNING id",
			run: func(q *Query) error {
				return q.First(&retryQueryModel{})
			},
		},
		"update returning many": {
			method:    "SelectContext",
			statement: "UPDATE retry_query_models SET id = id RETURNING id",
			run: func(q *Query) error {
				return q.All(&[]retryQueryModel{})
			},
		},
		"delete returning": {
			method:    "GetContext",
			statement: "DELETE FROM retry_query_models RETURNING id",
			run: func(q *Query) error {
				return q.First(&retryQueryModel{})
			},
		},
		"data modifying common table expression": {
			method:    "GetContext",
			statement: "WITH changed AS (UPDATE retry_query_models SET id = id RETURNING id) SELECT id FROM changed",
			run: func(q *Query) error {
				return q.First(&retryQueryModel{})
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := newCountingStore(2, closedErr())
			err := tc.run(newRetryQueryTestConnection(t, inner).RawQuery(tc.statement))
			assert.Error(t, err)
			assert.Equal(t, 1, inner.calls[tc.method], "raw SQL must not be replayed without an explicit read-only assertion")
		})
	}
}

func TestDirectRawSQLMutationIsOneShot(t *testing.T) {
	for name, query := range map[string]func(*Connection) *Query{
		"generated query changed to raw SQL": func(c *Connection) *Query {
			q := Q(c)
			q.RawSQL.Fragment = "UPDATE retry_query_models SET id = id RETURNING id"
			return q
		},
		"asserted raw query changed afterwards": func(c *Connection) *Query {
			q := c.RawQuery("SELECT id FROM retry_query_models").RetryableRead()
			q.RawSQL.Fragment = "UPDATE retry_query_models SET id = id RETURNING id"
			return q
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := newCountingStore(2, closedErr())
			err := query(newRetryQueryTestConnection(t, inner)).First(&retryQueryModel{})
			require.Error(t, err)
			assert.Equal(t, 1, inner.calls["GetContext"], "raw SQL must require an explicit read-only assertion for the executed statement")
		})
	}
}

func TestQueryReusedAfterDeleteIsOneShot(t *testing.T) {
	t.Parallel()
	inner := newCountingStore(2, closedErr())
	q := newRetryQueryTestConnection(t, inner).Where("id = ?", 1)
	require.Error(t, q.Delete(&retryQueryModel{}))

	err := q.First(&retryQueryModel{})
	require.Error(t, err)
	assert.Equal(t, 1, inner.calls["GetContext"], "a delete operation must never be replayed through a read executor")
}

func TestRetryableReadOptsRawQueryIntoRetries(t *testing.T) {
	for name, tc := range map[string]struct {
		method string
		run    func(*Query) error
	}{
		"first": {
			method: "GetContext",
			run: func(q *Query) error {
				return q.First(&retryQueryModel{})
			},
		},
		"all": {
			method: "SelectContext",
			run: func(q *Query) error {
				return q.All(&[]retryQueryModel{})
			},
		},
		"exists": {
			method: "Get",
			run: func(q *Query) error {
				_, err := q.Exists(&retryQueryModel{})
				return err
			},
		},
		"count": {
			method: "Get",
			run: func(q *Query) error {
				_, err := q.Count(&retryQueryModel{})
				return err
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := newCountingStore(2, closedErr())
			q := newRetryQueryTestConnection(t, inner).RawQuery("SELECT id FROM retry_query_models").RetryableRead()
			require.NoError(t, tc.run(q))
			assert.Equal(t, 3, inner.calls[tc.method])
		})
	}
}

func TestReadsInsideTransactionAreNotRetried(t *testing.T) {
	t.Parallel()
	inner := newCountingStore(2, closedErr())
	c := newRetryQueryTestConnection(t, inner)
	c.TX = &Tx{}

	err := c.Where("id = ?", 1).First(&retryQueryModel{})
	require.Error(t, err)
	assert.Equal(t, 1, inner.calls["GetContext"])
}

func TestReadRetryPreservesCancellationAndConnectionErrors(t *testing.T) {
	for name, cancelAt := range map[string]int{
		"before retry":     1,
		"on final attempt": closedConnectionAttempts,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			injected := closedErr()
			inner := newCountingStore(1<<30, injected)
			inner.onCall = func() {
				if inner.calls["GetContext"] == cancelAt {
					cancel()
				}
			}
			c := newRetryQueryTestConnection(t, inner).WithContext(ctx)

			err := c.Where("id = ?", 1).First(&retryQueryModel{})
			require.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled)
			assert.ErrorIs(t, err, injected)
			assert.Equal(t, cancelAt, inner.calls["GetContext"])
		})
	}
}

func TestReadRetryPreservesConnectionErrorWhenDriverReturnsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	injected := closedErr()
	inner := newCountingStore(1<<30, injected)
	inner.onCall = func() {
		if inner.calls["GetContext"] == 2 {
			cancel()
			inner.err = context.Canceled
		}
	}

	err := newRetryQueryTestConnection(t, inner).WithContext(ctx).First(&retryQueryModel{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, injected)
	assert.Equal(t, 2, inner.calls["GetContext"])
}

func TestReadRetryPreservesConnectionErrorOnLaterFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		terminal            error
		wantConnectionCause bool
	}{
		"driver cancellation while the caller context is live": {
			terminal:            fmt.Errorf("driver: %w", context.Canceled),
			wantConnectionCause: true,
		},
		"driver deadline while the caller context is live": {
			terminal:            fmt.Errorf("driver: %w", context.DeadlineExceeded),
			wantConnectionCause: true,
		},
		"unrelated failure": {terminal: errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			connErr := closedErr()
			inner := newCountingStore(1<<30, connErr)
			inner.onCall = func() {
				if inner.calls["GetContext"] == 2 {
					inner.err = tc.terminal
				}
			}

			err := newRetryQueryTestConnection(t, inner).WithContext(t.Context()).First(&retryQueryModel{})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.terminal, "the terminal error must reach the caller")
			if tc.wantConnectionCause {
				assert.ErrorIs(t, err, connErr, "a context error must retain the connection error that caused its retry")
			} else {
				assert.NotErrorIs(t, err, connErr, "an unrelated terminal error must be returned without a stale retry cause")
			}
			assert.False(t, IsConnectionClosed(err), "a terminal retry result must not start another connection retry loop")
			assert.Equal(t, 2, inner.calls["GetContext"])
		})
	}
}

func TestReadRetryPreservesCallerContextReason(t *testing.T) {
	t.Run("caller cancellation and driver deadline", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		connErr := closedErr()
		inner := newCountingStore(1<<30, connErr)
		inner.onCall = func() {
			if inner.calls["GetContext"] == 2 {
				cancel()
				inner.err = context.DeadlineExceeded
			}
		}

		err := newRetryQueryTestConnection(t, inner).WithContext(ctx).First(&retryQueryModel{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorIs(t, err, connErr)
		assert.False(t, IsConnectionClosed(err))
	})

	t.Run("caller deadline and driver cancellation", func(t *testing.T) {
		t.Parallel()
		baseCtx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		ctx := deadlineErrorContext{Context: baseCtx}
		connErr := closedErr()
		inner := newCountingStore(1<<30, connErr)
		inner.onCall = func() {
			if inner.calls["GetContext"] == 2 {
				cancel()
				inner.err = context.Canceled
			}
		}

		err := newRetryQueryTestConnection(t, inner).WithContext(ctx).First(&retryQueryModel{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorIs(t, err, connErr)
		assert.False(t, IsConnectionClosed(err))
	})
}

func TestReadRetryDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()
	for name, injected := range map[string]error{
		"unrelated":             errors.New("boom"),
		"serialization failure": &pgconn.PgError{Code: "40001"},
		"unique violation":      &pgconn.PgError{Code: "23505"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := newCountingStore(1, injected)
			err := newRetryQueryTestConnection(t, inner).First(&retryQueryModel{})
			assert.ErrorIs(t, err, injected)
			assert.Equal(t, 1, inner.calls["GetContext"])
		})
	}
}

func TestReadRetryReturnsLastErrorAfterExhaustion(t *testing.T) {
	t.Parallel()
	inner := newCountingStore(1<<30, closedErr())
	err := newRetryQueryTestConnection(t, inner).First(&retryQueryModel{})
	require.Error(t, err)
	assert.Equal(t, "57P01", SQLState(err))
	assert.Equal(t, closedConnectionAttempts, inner.calls["GetContext"])
}

func TestReadRetryRunsCallbacksOnce(t *testing.T) {
	t.Parallel()
	inner := newCountingStore(2, closedErr())
	model := &retryCallbackModel{}
	require.NoError(t, newRetryQueryTestConnection(t, inner).First(model))
	assert.Equal(t, 1, model.afterFindCalls)
}

type retryCallbackModel struct {
	ID             int `db:"id"`
	afterFindCalls int
}

func (m *retryCallbackModel) AfterFind(*Connection) error {
	m.afterFindCalls++
	return nil
}

func TestReadRetryTelemetryDistinguishesAbandonedFromExhausted(t *testing.T) {
	for name, tc := range map[string]struct {
		cancelAfter   int
		wantRetries   int64
		wantAbandoned bool
	}{
		"cancelled before any retry": {cancelAfter: 1, wantRetries: 0, wantAbandoned: true},
		"cancelled after one retry":  {cancelAfter: 2, wantRetries: 1, wantAbandoned: true},
		"budget exhausted":           {cancelAfter: 0, wantRetries: closedConnectionAttempts - 1, wantAbandoned: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			ctx, span := tp.Tracer("test").Start(t.Context(), "op")
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			inner := newCountingStore(1<<30, closedErr())
			calls := 0
			inner.onCall = func() {
				calls++
				if tc.cancelAfter > 0 && calls == tc.cancelAfter {
					cancel()
				}
			}

			require.Error(t, newRetryQueryTestConnection(t, inner).WithContext(ctx).
				Where("id = ?", 1).First(&retryQueryModel{}))
			span.End()

			var attrs map[string]any
			for _, s := range recorder.Ended() {
				for _, e := range s.Events() {
					if e.Name != "db.connection.closed.retry" {
						continue
					}
					attrs = map[string]any{}
					for _, a := range e.Attributes {
						attrs[string(a.Key)] = a.Value.AsInterface()
					}
				}
			}
			require.NotNil(t, attrs, "the closed-connection event must be recorded either way")
			assert.Equal(t, tc.wantRetries, attrs["retries"])
			assert.Equal(t, false, attrs["recovered"])

			_, abandoned := attrs["abandoned"]
			assert.Equal(t, tc.wantAbandoned, abandoned,
				"a run stopped by the context must be distinguishable from a spent budget")
		})
	}
}

func TestReadRetryTelemetryDoesNotInferAbandonment(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := tp.Tracer("test").Start(t.Context(), "op")
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)

	injected := errors.New("boom")
	connErr := closedErr()
	inner := newCountingStore(1<<30, connErr)
	inner.onCall = func() {
		if inner.calls["GetContext"] == 2 {
			cancel()
			inner.err = injected
		}
	}

	err := newRetryQueryTestConnection(t, inner).WithContext(ctx).First(&retryQueryModel{})
	assert.ErrorIs(t, err, injected)
	assert.NotErrorIs(t, err, connErr, "an unrelated terminal error must not inherit a stale connection cause")
	assert.False(t, IsConnectionClosed(err))
	span.End()

	for _, s := range recorder.Ended() {
		for _, event := range s.Events() {
			if event.Name != "db.connection.closed.retry" {
				continue
			}
			for _, attr := range event.Attributes {
				assert.NotEqual(t, "abandoned", string(attr.Key))
			}
			return
		}
	}
	t.Fatal("the closed-connection event was not recorded")
}
