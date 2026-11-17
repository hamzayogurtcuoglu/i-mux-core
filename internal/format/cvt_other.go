//go:build !arm64

package format

import "math"

// cLongLong is C's (long long) conversion of a double as amd64 does it (CVTTSD2SI): out of
// range and NaN give the "integer indefinite" value, the smallest int64.
func cLongLong(f float64) int64 {
	switch {
	case math.IsNaN(f), f >= math.MaxInt64, f < math.MinInt64:
		return math.MinInt64
	}
	return int64(f)
}
