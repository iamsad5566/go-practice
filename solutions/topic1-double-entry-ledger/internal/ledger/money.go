package ledger

import "math"

// checkedAdd adds two non-negative amounts, reporting false on int64 overflow.
func checkedAdd(a, b int64) (int64, bool) {
	if a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}
