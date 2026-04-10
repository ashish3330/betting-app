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

	if err := m.Suspend(); err != nil {
		t.Fatalf("Suspend() from open: unexpected error: %v", err)
	}
	if m.Status != MarketSuspended {
		t.Errorf("after Suspend(), status = %v, want suspended", m.Status)
	}

	if err := m.Resume(); err != nil {
		t.Fatalf("Resume() from suspended: unexpected error: %v", err)
	}
	if m.Status != MarketOpen {
		t.Errorf("after Resume(), status = %v, want open", m.Status)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close() from open: unexpected error: %v", err)
	}
	if m.Status != MarketClosed {
		t.Errorf("after Close(), status = %v, want closed", m.Status)
	}
}

func TestMarket_InvalidTransitions(t *testing.T) {
	// Cannot suspend from closed
	m := &Market{Status: MarketClosed}
	if err := m.Suspend(); err == nil {
		t.Error("Suspend() from closed: expected error, got nil")
	}

	// Cannot resume from open
	m = &Market{Status: MarketOpen}
	if err := m.Resume(); err == nil {
		t.Error("Resume() from open: expected error, got nil")
	}

	// Cannot close from settled
	m = &Market{Status: MarketSettled}
	if err := m.Close(); err == nil {
		t.Error("Close() from settled: expected error, got nil")
	}
}
