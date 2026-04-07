package cashout

import (
	"math"
	"testing"
)

func TestCalculateCashoutAmount(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name          string
		originalStake float64
		originalPrice float64
		currentPrice  float64
		side          string
		wantMin       float64
		wantMax       float64
	}{
		{
			name:          "back bet - price moved in favor",
			originalStake: 100,
			originalPrice: 2.0,
			currentPrice:  1.5,
			side:          "back",
			wantMin:       120, // (2.0/1.5)*100*0.95 = 126.67
			wantMax:       140,
		},
		{
			name:          "back bet - price moved against",
			originalStake: 100,
			originalPrice: 2.0,
			currentPrice:  3.0,
			side:          "back",
			wantMin:       50, // (2.0/3.0)*100*0.95 = 63.33
			wantMax:       80,
		},
		{
			name:          "back bet - same price",
			originalStake: 100,
			originalPrice: 2.0,
			currentPrice:  2.0,
			side:          "back",
			wantMin:       90, // (2.0/2.0)*100*0.95 = 95
			wantMax:       100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CalculateCashoutAmount(tt.originalStake, tt.originalPrice, tt.currentPrice, tt.side)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CalculateCashoutAmount() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
			// Verify it's properly rounded to 2 decimal places
			if got != math.Round(got*100)/100 {
				t.Errorf("CalculateCashoutAmount() = %v, not rounded to 2 decimal places", got)
			}
		})
	}
}
