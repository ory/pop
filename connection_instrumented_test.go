package pop

import (
	"os"
	"slices"

	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func testInstrumentedDriver(p *suite.Suite) {
	var (
		queryMySQL = "SELECT 1 FROM DUAL WHERE 1=?"
		query      = "SELECT 1 WHERE 1=?"
		expected   = []string{
			"SELECT 1 FROM DUAL WHERE 1=?",
			"SELECT 1 FROM DUAL WHERE 1=$1",
			"SELECT 1 WHERE 1=?",
			"SELECT 1 WHERE 1=$1",
		}
		r        = p.Require()
		recorder = tracetest.NewSpanRecorder()
		provider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		tracer   = provider.Tracer("test")
		deets    = *Connections[os.Getenv("SODA_DIALECT")].Dialect.Details()
	)
	deets.TracerProvider = provider
	if os.Getenv("SODA_DIALECT") == "mysql" {
		query = queryMySQL
	}

	c, err := NewConnection(&deets)
	r.NoError(err)
	r.NoError(c.Open())

	ctx, span := tracer.Start(p.T().Context(), "parent")

	err = c.WithContext(ctx).RawQuery(query, 1).Exec()
	r.NoError(err)

	span.End()

	spans := recorder.Ended()
	var found bool
	for _, span := range spans {
		if span.Name() == "parent" {
			continue
		}

		for _, e := range expected {
			if slices.ContainsFunc(span.Attributes(), func(a attribute.KeyValue) bool {
				return string(a.Key) == "db.statement" && a.Value.AsString() == e
			}) {
				found = true
				break
			}
		}
	}
	r.True(found)
}

func (s *PostgreSQLSuite) Test_Instrumentation() {
	testInstrumentedDriver(&s.Suite)
}

func (s *MySQLSuite) Test_Instrumentation() {
	testInstrumentedDriver(&s.Suite)
}

func (s *SQLiteSuite) Test_Instrumentation() {
	testInstrumentedDriver(&s.Suite)
}

func (s *CockroachSuite) Test_Instrumentation() {
	testInstrumentedDriver(&s.Suite)
}
