package models

import (
	"testing"
)

func TestPlaceBetRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     PlaceBetRequest
		wantErr bool
	}{
		{
			name: "valid back bet",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       2.5,
				Stake:       100,
				ClientRef:   "ref-001",
			},
			wantErr: false,
		},
		{
			name: "valid lay bet",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideLay,
				Price:       3.0,
				Stake:       50,
				ClientRef:   "ref-002",
			},
			wantErr: false,
		},
		{
			name: "missing market_id",
			req: PlaceBetRequest{
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       2.5,
				Stake:       100,
				ClientRef:   "ref-003",
			},
			wantErr: true,
		},
		{
			name: "invalid side",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        "win",
				Price:       2.5,
				Stake:       100,
				ClientRef:   "ref-004",
			},
			wantErr: true,
		},
		{
			name: "price too low",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       1.0,
				Stake:       100,
				ClientRef:   "ref-005",
			},
			wantErr: true,
		},
		{
			name: "price exactly 1.0",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       1.0,
				Stake:       100,
				ClientRef:   "ref-005b",
			},
			wantErr: true,
		},
		{
			name: "zero stake",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       2.5,
				Stake:       0,
				ClientRef:   "ref-006",
			},
			wantErr: true,
		},
		{
			name: "negative stake",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       2.5,
				Stake:       -100,
				ClientRef:   "ref-007",
			},
			wantErr: true,
		},
		{
			name: "missing client_ref",
			req: PlaceBetRequest{
				MarketID:    "market-1",
				SelectionID: 1,
				Side:        BetSideBack,
				Price:       2.5,
				Stake:       100,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBet_Liability(t *testing.T) {
	tests := []struct {
		name string
		bet  Bet
		want float64
	}{
		{
			name: "back bet liability equals stake",
			bet:  Bet{Side: BetSideBack, Stake: 100, Price: 3.5},
			want: 100,
		},
		{
			name: "lay bet liability = stake * (price - 1)",
			bet:  Bet{Side: BetSideLay, Stake: 100, Price: 3.5},
			want: 250,
		},
		{
			name: "lay bet at low odds",
			bet:  Bet{Side: BetSideLay, Stake: 200, Price: 1.5},
			want: 100,
		},
		{
			name: "back bet small stake",
			bet:  Bet{Side: BetSideBack, Stake: 10, Price: 5.0},
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.bet.Liability()
			if got != tt.want {
				t.Errorf("Liability() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBet_PotentialProfit(t *testing.T) {
	tests := []struct {
		name string
		bet  Bet
		want float64
	}{
		{
			name: "back bet profit = stake * (price - 1)",
			bet:  Bet{Side: BetSideBack, Stake: 100, Price: 3.5},
			want: 250,
		},
		{
			name: "lay bet profit equals stake",
			bet:  Bet{Side: BetSideLay, Stake: 100, Price: 3.5},
			want: 100,
		},
		{
			name: "back at even odds",
			bet:  Bet{Side: BetSideBack, Stake: 50, Price: 2.0},
			want: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.bet.PotentialProfit()
			if got != tt.want {
				t.Errorf("PotentialProfit() = %v, want %v", got, tt.want)
			}
		})
	}
}
