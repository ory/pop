package pop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type Person struct {
	ID        string `db:"id"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
}

func (p Person) Person() string {
	return "people"
}

func TestForceIndexAllDialects(t *testing.T) {
	r := require.New(t)

	connectionDetails := []ConnectionDetails{
		{Dialect: "sqlite", URL: ":memory:"},
		{Dialect: "mysql", Database: "mysql", Host: "127.0.0.1", Port: "3444", User: "root", Password: "secret"},
		{Dialect: "postgres", Database: "postgres", Host: "127.0.0.1", Port: "3445", User: "postgres", Password: "secret"},
		{Dialect: "cockroach", Database: "defaultdb", Host: "127.0.0.1", Port: "3446", User: "root", Password: "secret"},
	}

	for _, cd := range connectionDetails {
		t.Run(cd.Dialect, func(t *testing.T) {
			conn, err := NewConnection(&cd)
			r.NoError(err)
			r.NotNil(conn)

			err = conn.Open()
			r.NoError(err)

			err = conn.RawQuery(`DROP TABLE IF EXISTS people`).Exec()
			r.NoError(err)
			err = conn.RawQuery(`CREATE TABLE people(id VARCHAR(256) NOT NULL, first_name VARCHAR(256) NOT NULL, last_name VARCHAR(256) NOT NULL)`).Exec()
			r.NoError(err)

			err = conn.RawQuery(`CREATE INDEX people_idx ON people(first_name, last_name)`).Exec()
			r.NoError(err)

			{
				personCreate := Person{ID: "foo", FirstName: "George", LastName: "Washington"}
				err = conn.Create(&personCreate)
				r.NoError(err)
			}
			{
				personCreate := Person{ID: "bar", FirstName: "Franklin", LastName: "Roosevelt"}
				err = conn.Create(&personCreate)
				r.NoError(err)
			}

			var personSelect Person
			err = conn.Select("last_name").Where("first_name = ?", "Franklin").ForceIndex("people_idx").First(&personSelect)
			r.NoError(err)

			r.Equal(personSelect.LastName, "Roosevelt")
		})
	}
}
