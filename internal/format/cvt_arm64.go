//go:build arm64

package format

import "math"

// cLongLong is C's (long long) conversion of a double as arm64 does it (FCVTZS): out of range
// saturates, NaN is 0.
func cLongLong(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f < math.MinInt64:
		return math.MinInt64
	}
	return int64(f)
}
