package pg

import "testing"

// Page bounds are enforced here and nowhere else (a duplicate cap in the service layer
// silently re-capped an over-max limit to the default — see BUG-32).
func TestClampLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int32
		want int32
	}{
		{"unset falls back to the default", 0, 50},
		{"negative falls back to the default", -10, 50},
		{"in range passes through", 100, 100},
		{"at the ceiling passes through", 200, 200},
		{"over the ceiling clamps DOWN, not to the default", 500, 200},
	} {
		if got := clampLimit(tc.in); got != tc.want {
			t.Errorf("%s: clampLimit(%d) = %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestClampOffset(t *testing.T) {
	for _, tc := range []struct{ in, want int32 }{
		{-5, 0}, // Postgres errors on a negative OFFSET
		{0, 0},
		{450, 450},
	} {
		if got := clampOffset(tc.in); got != tc.want {
			t.Errorf("clampOffset(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
