package cost

import "testing"

// Weighted counts uncached input once, cached input at a tenth and output
// at five times, rounded up to a whole token.
func TestWeighted(t *testing.T) {
	for _, tc := range []struct {
		u    Usage
		want int64
	}{
		{Usage{}, 0},
		{Usage{In: 1000}, 1000},
		{Usage{CachedIn: 1000}, 100},
		{Usage{Out: 1000}, 5000},
		{Usage{In: 1000, CachedIn: 1000, Out: 1000}, 6100},
		// A tenth of one cached token rounds up to one.
		{Usage{CachedIn: 1}, 1},
		{Usage{CachedIn: 10}, 1},
		{Usage{CachedIn: 11}, 2},
		{Usage{In: 7, CachedIn: 19, Out: 3}, 7 + 2 + 15},
		// Calls and time weigh nothing.
		{Usage{Calls: 9, MS: 90000}, 0},
	} {
		if got := tc.u.Weighted(); got != tc.want {
			t.Errorf("%+v weighs %d, want %d", tc.u, got, tc.want)
		}
	}
}

// Add sums every field.
func TestAdd(t *testing.T) {
	u := Usage{In: 1, CachedIn: 2, Out: 3, Calls: 4, MS: 5}
	u.Add(Usage{In: 10, CachedIn: 20, Out: 30, Calls: 40, MS: 50})
	if u != (Usage{In: 11, CachedIn: 22, Out: 33, Calls: 44, MS: 55}) {
		t.Fatalf("%+v", u)
	}
}

// An estimate is a token for each four bytes, rounded up, all of it
// uncached input, and weighs what it reads.
func TestEstimate(t *testing.T) {
	for bytes, want := range map[int]int64{0: 0, 1: 1, 4: 1, 5: 2, 4000: 1000, 4001: 1001} {
		e := Estimate(bytes)
		if e != (Usage{In: want}) || e.Weighted() != want {
			t.Errorf("Estimate(%d) = %+v weighing %d, want %d", bytes, e, e.Weighted(), want)
		}
	}
}

// PerChat divides the week's weighted tokens among the chats it
// organized, rounded to the nearest, and is 0 with none.
func TestPerChat(t *testing.T) {
	for _, tc := range []struct {
		w    Week
		want int64
	}{
		{Week{Usage: Usage{In: 9000}}, 0},
		{Week{Usage: Usage{In: 9000}, Organized: 3}, 3000},
		{Week{Usage: Usage{In: 10}, Organized: 4}, 3},
		{Week{Usage: Usage{In: 10}, Organized: 3}, 3},
		{Week{Usage: Usage{In: 11}, Organized: 2}, 6},
		{Week{Usage: Usage{In: 1000, CachedIn: 1000, Out: 1000}, Organized: 2}, 3050},
	} {
		if got := tc.w.PerChat(); got != tc.want {
			t.Errorf("%+v per chat %d, want %d", tc.w, got, tc.want)
		}
	}
}
