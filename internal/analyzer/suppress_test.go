package analyzer

import "testing"

func TestSuppressIfClose(t *testing.T) {
	tests := []struct {
		name        string
		current     float64
		recommended float64
		tolerance   float64
		want        bool
	}{
		{"identical values", 100, 100, 0.10, true},
		{"5% off, 10% tolerance", 100, 105, 0.10, true},
		{"15% off, 10% tolerance", 100, 115, 0.10, false},
		{"current=0 treated as max(c,1)", 0, 0.05, 0.10, true},   // (0.05-0)/max(0,1) = 0.05
		{"current=0, big recommended", 0, 10, 0.10, false},
		{"negative direction (current high, rec low)", 100, 95, 0.10, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SuppressIfClose(tc.current, tc.recommended, tc.tolerance)
			if got != tc.want {
				t.Errorf("SuppressIfClose(%v, %v, %v) = %v, want %v",
					tc.current, tc.recommended, tc.tolerance, got, tc.want)
			}
		})
	}
}
