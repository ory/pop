package pop

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// closedConnectionAttempts is the total number of times a read runs before its
// error reaches the caller.
const closedConnectionAttempts = 5

func (q *Query) retryRead(ctx context.Context, read func() error) error {
	if !q.canRetryRead() || q.Connection.TX != nil {
		return read()
	}

	var (
		err               error
		lastConnectionErr error
		sqlState          string
	)
	for attempt := range closedConnectionAttempts {
		if attempt > 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = retriedReadError(ctxErr, lastConnectionErr)
				recordClosedConnectionRetries(ctx, attempt-1, sqlState, err, ctxErr)
				return err
			}
		}

		err = read()
		if err == nil || !IsConnectionClosed(err) {
			if attempt > 0 {
				if err != nil {
					if !isContextError(err) {
						recordClosedConnectionRetries(ctx, attempt, sqlState, err, nil)
						return err
					}
					var abandoned error
					if ctxErr := ctx.Err(); ctxErr != nil {
						abandoned = ctxErr
						if !errors.Is(err, ctxErr) {
							err = retriedReadError(ctxErr, err)
						}
					}
					err = retriedReadError(err, lastConnectionErr)
					recordClosedConnectionRetries(ctx, attempt, sqlState, err, abandoned)
					return err
				}
				recordClosedConnectionRetries(ctx, attempt, sqlState, nil, nil)
			}
			return err
		}
		lastConnectionErr = err
		if sqlState == "" {
			sqlState = SQLState(err)
		}
		if ctx.Err() != nil {
			ctxErr := ctx.Err()
			err = retriedReadError(ctxErr, err)
			recordClosedConnectionRetries(ctx, attempt, sqlState, err, ctxErr)
			return err
		}
		if attempt == closedConnectionAttempts-1 {
			break
		}

		timer := time.NewTimer(time.Duration(attempt+1) * time.Duration(5+rand.IntN(10)) * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			ctxErr := ctx.Err()
			err = retriedReadError(ctxErr, err)
			recordClosedConnectionRetries(ctx, attempt, sqlState, err, ctxErr)
			return err
		case <-timer.C:
		}
	}
	recordClosedConnectionRetries(ctx, closedConnectionAttempts-1, sqlState, err, nil)
	return err
}

func (q *Query) canRetryRead() bool {
	if q.Operation != Select {
		return false
	}
	if q.RawSQL != nil && q.RawSQL.Fragment != "" {
		return q.retryableRawSQL != "" && q.retryableRawSQL == q.RawSQL.Fragment
	}
	return q.retryableRead
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// retriedReadError keeps both errors that ended a retry reachable to callers.
func retriedReadError(err, cause error) error {
	return fmt.Errorf("%w: %w", err, cause)
}

func recordClosedConnectionRetries(ctx context.Context, retries int, sqlState string, err, abandoned error) {
	attrs := []attribute.KeyValue{
		attribute.Int("retries", retries),
		attribute.Bool("recovered", err == nil),
	}
	if sqlState != "" {
		attrs = append(attrs, attribute.String("db.response.status_code", sqlState))
	}
	if abandoned != nil {
		attrs = append(attrs, attribute.String("abandoned", abandoned.Error()))
	}
	trace.SpanFromContext(ctx).AddEvent("db.connection.closed.retry", trace.WithAttributes(attrs...))
}
