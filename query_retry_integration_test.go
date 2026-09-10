package pop

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serverClosedOnceStore struct {
	store
	victim   *sqlx.Conn
	attempts int
	firstErr error
}

func (s *serverClosedOnceStore) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	s.attempts++
	if s.attempts == 1 {
		s.firstErr = sqlx.GetContext(ctx, s.victim, dest, query, args...)
		return s.firstErr
	}
	return s.store.GetContext(ctx, dest, query, args...)
}

func (s *CockroachSuite) TestGeneratedReadRetriesServerSideClose() {
	t := s.T()
	ctx := t.Context()

	id := fmt.Sprintf("retry-%d", time.Now().UnixNano())
	require.NoError(t, PDB.RawQuery("INSERT INTO clients (id) VALUES (?)", id).Exec())
	t.Cleanup(func() {
		_ = PDB.RawQuery("DELETE FROM clients WHERE id = ?", id).Exec()
	})

	victim, err := sqlx.NewDb(PDB.Store.SQLDB(), "pgx").Connx(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = victim.Close() })

	admin, err := sql.Open("pgx", strings.Replace(PDB.URL(), "cockroach://", "postgres://", 1))
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(ctx))

	var sessionID string
	require.NoError(t, victim.QueryRowContext(ctx, "SHOW session_id").Scan(&sessionID))
	_, err = admin.ExecContext(ctx, "CANCEL SESSION $1", sessionID)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var n int
		assert.Error(collect, victim.QueryRowContext(ctx, "SELECT 1").Scan(&n))
	}, 5*time.Second, 100*time.Millisecond)

	retrying := &serverClosedOnceStore{store: PDB.Store, victim: victim}
	c := PDB.copy()
	c.Store = retrying
	var got Client
	require.NoError(t, c.WithContext(ctx).Where("id = ?", id).First(&got))
	assert.Equal(t, id, got.ClientID)
	assert.Equal(t, 2, retrying.attempts)
	assert.True(t, IsConnectionClosed(retrying.firstErr), "first attempt returned %T: %v", retrying.firstErr, retrying.firstErr)
}
