package models

import "testing"

func TestMarket_CanAcceptBets(t *testing.T) {
	tests := []struct {
		status MarketStatus
		want   bool
	}{
		{MarketOpen, true},
		{MarketSuspended, false},
		{MarketClosed, false},
		{MarketSettled, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			m := &Market{Status: tt.status}
			if got := m.CanAcceptBets(); got != tt.want {
				t.Errorf("CanAcceptBets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarket_StatusTransitions(t *testing.T) {
	m := &Market{Status: MarketOpen}

	m.Suspend()
	if m.Status != MarketSuspended {
		t.Errorf("after Suspend(), status = %v, want suspended", m.Status)
	}

	m.Resume()
	if m.Status != MarketOpen {
		t.Errorf("after Resume(), status = %v, want open", m.Status)
	}

	m.Close()
	if m.Status != MarketClosed {
		t.Errorf("after Close(), status = %v, want closed", m.Status)
	}
}
