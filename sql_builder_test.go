package pop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type v struct{}
type vv []v
type vptr *v
type vvptr []*v

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name   string
		val    any
		want   string
		wantOK bool
	}{{
		name:   "struct value",
		val:    v{},
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name:   "struct pointer",
		val:    &v{},
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name:   "vptr",
		val:    vptr(nil),
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name:   "slice of struct values",
		val:    vv{},
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name:   "slice of struct pointers",
		val:    vvptr{},
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name:   "slice of struct pointer nil",
		val:    vvptr{nil},
		want:   "github.com/ory/pop/v6.v",
		wantOK: true,
	}, {
		name: "string",
		val:  "hello",
	}, {
		name: "nil",
		val:  nil,
	}, {
		name: "int",
		val:  42,
	}, {
		name: "int pointer",
		val:  func() *int { i := 42; return &i }(),
	}, {
		name: "map",
		val:  map[string]int{"a": 1},
	}}

	for _, tc := range tests {
		t.Run("case="+tc.name, func(t *testing.T) {
			got, ok := cacheKey(tc.val)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
