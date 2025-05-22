package pop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type Person struct {
	ID   int    `db:"id"`
	Name string `db:"name"`
}

func (p Person) Person() string {
	return "people"
}

func TestForceIndexAllDialects(t *testing.T) {
	r := require.New(t)

	connectionDetails := []ConnectionDetails{
		{Dialect: "sqlite", URL: ":memory:"},
		{Dialect: "postgres", Database: "pop_test", Host: "127.0.0.1", Port: 5433, User: "postgres", Password: "postgres"},
		{Dialect: "mysql", Database: "pop_test", Host: "127.0.0.1", Port: 5433, User: "pop", Password: "pop"},
		// {Dialect: "mysql", URL: "mysql://127.0.0.1:3307/pop_test"},
	}

	for _, cd := range connectionDetails {
		conn, err := NewConnection(&cd)
		r.NoError(err)
		r.NotNil(conn)

		err = conn.Open()
		r.NoError(err)

		err = conn.RawQuery(`DROP TABLE IF EXISTS people`).Exec()
		r.NoError(err)
		err = conn.RawQuery(`CREATE TABLE people(id INTEGER PRIMARY KEY NOT NULL, name text NOT NULL)`).Exec()
		r.NoError(err)

		err = conn.RawQuery(`CREATE INDEX people_idx ON people(id, name)`).Exec()
		r.NoError(err)

		{
			personCreate := Person{ID: 1, Name: "Joe"}
			err = conn.Create(&personCreate)
			r.NoError(err)
		}
		{
			personCreate := Person{ID: 2, Name: "Jill"}
			err = conn.Create(&personCreate)
			r.NoError(err)
		}

		var personSelect Person
		err = conn.Select("id").Where("name = ?", "Joe").ForceIndex("people_idx").First(&personSelect)
		r.NoError(err)

		r.Equal(personSelect.ID, 1)
	}
}
