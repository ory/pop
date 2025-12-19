package pop

import "testing"

type v struct{}
type vv []v
type vptr *v
type vvptr []*v

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{{
		name: "struct value",
		val:  v{},
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "struct pointer",
		val:  &v{},
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "vptr",
		val:  vptr(nil),
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "slice of struct values",
		val:  vv{},
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "slice of struct pointers",
		val:  vvptr{},
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "slice of struct pointer nil",
		val:  vvptr{nil},
		want: "github.com/ory/pop/v6.v",
	}, {
		name: "string",
		val:  "hello",
		want: "builtin.string",
	}, {
		name: "nil",
		val:  nil,
		want: "builtin.nil",
	}, {
		name: "int",
		val:  42,
		want: "builtin.int",
	}, {
		name: "int pointer",
		val:  func() *int { i := 42; return &i }(),
		want: "builtin.int",
	}, {
		name: "map",
		val:  map[string]int{"a": 1},
		want: "builtin.map",
	}}

	for _, tc := range tests {
		t.Run("case="+tc.name, func(t *testing.T) {
			got := cacheKey(tc.val)
			if got != tc.want {
				t.Errorf("cacheKey() = %v, want %v", got, tc.want)
			}
		})
	}
}
