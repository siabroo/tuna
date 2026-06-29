package analyzer

import "math"

// SuppressIfClose reports whether |current - recommended| / max(current, 1)
// is within tolerance. Used to filter out trivial-difference
// recommendations (default tolerance 0.10 = 10%, per spec §6 closing).
func SuppressIfClose(current, recommended, tolerance float64) bool {
	denom := math.Max(current, 1.0)
	delta := math.Abs(current-recommended) / denom
	return delta < tolerance
}
