package pop

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func Test_Connection_SimpleFlow(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite:///foo.db",
	}
	c, err := NewConnection(cd)
	r.NoError(err)

	err = c.Open()
	r.NoError(err)
	err = c.Open() // open again
	r.NoError(err)
	err = c.Close()
	r.NoError(err)
}

func Test_Connection_Open_Close_Reopen(t *testing.T) {
	r := require.New(t)

	c, err := NewConnection(&ConnectionDetails{
		URL: "sqlite://file::memory:?_fk=true",
	})
	r.NoError(err)

	for i := 0; i < 2; i++ {
		r.NoError(c.Open())
		r.NoError(c.Transaction(func(c *Connection) error { return nil }))
		r.NoError(c.Close())
	}
}

func Test_Connection_Open_NoDialect(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite:///foo.db",
	}
	c, err := NewConnection(cd)
	r.NoError(err)

	c.Dialect = nil
	err = c.Open()
	r.Error(err)
}

func Test_Connection_Open_BadDriver(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		URL: "sqlite:///foo.db",
	}
	c, err := NewConnection(cd)
	r.NoError(err)

	cd.Driver = "unknown"
	err = c.Open()
	r.Error(err)
}

func Test_Connection_NewTransaction(t *testing.T) {
	r := require.New(t)
	ctx := context.WithValue(context.Background(), "test", "test")

	c, err := NewConnection(&ConnectionDetails{
		URL: "sqlite://file::memory:?_fk=true",
	})
	r.NoError(err)
	r.NoError(c.Open())
	c = c.WithContext(ctx)

	t.Run("func=NewTransaction", func(t *testing.T) {
		r := require.New(t)
		tx, err := c.NewTransaction()
		r.NoError(err)

		// has transaction and context
		r.NotNil(tx.TX)
		r.Nil(c.TX)
		r.Equal(ctx, tx.Context())

		// does not start a new transaction
		ntx, err := tx.NewTransaction()
		r.NoError(err)
		r.Equal(tx, ntx)

		r.NoError(tx.TX.Rollback())
	})

	t.Run("func=NewTransactionContext", func(t *testing.T) {
		r := require.New(t)
		nctx := context.WithValue(ctx, "nested", "test")
		tx, err := c.NewTransactionContext(nctx)
		r.NoError(err)

		// has transaction and context
		r.NotNil(tx.TX)
		r.Nil(c.TX)
		r.Equal(nctx, tx.Context())

		r.NoError(tx.TX.Rollback())
	})
}

func Test_Connection_Transaction(t *testing.T) {
	c, err := NewConnection(&ConnectionDetails{
		URL: "sqlite://file::memory:?_fk=true",
	})
	require.NoError(t, err)
	require.NoError(t, c.Open())

	t.Run("Success", func(t *testing.T) {
		err = c.Transaction(func(c *Connection) error {
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("Failed", func(t *testing.T) {
		err = c.Transaction(func(c *Connection) error {
			return fmt.Errorf("failed")
		})
		require.ErrorContains(t, err, "failed")
	})

	t.Run("Panic", func(t *testing.T) {
		require.PanicsWithValue(t, "inner function panic", func() {
			_ = c.Transaction(func(c *Connection) error {
				panic("inner function panic")
			})
		})
	})

	t.Run("context canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, err := NewConnection(&ConnectionDetails{
				URL: "sqlite://file::memory:?_fk=true",
			})
			require.NoError(t, err)
			require.NoError(t, c.Open())
			t.Cleanup(func() { _ = c.Close() })

			ctx, cancel := context.WithCancel(t.Context())
			err = c.WithContext(ctx).Transaction(func(c *Connection) error {
				cancel()
				synctest.Wait()
				return c.RawQuery("SELECT 1").Exec()
			})
			require.ErrorIs(t, err, context.Canceled)
		})
	})
}
