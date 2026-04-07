package models

import "testing"

func TestRole_IsValid(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleSuperAdmin, true},
		{RoleAdmin, true},
		{RoleMaster, true},
		{RoleAgent, true},
		{RoleClient, true},
		{"invalid", false},
		{"", false},
		{"ADMIN", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := tt.role.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRole_CanManage(t *testing.T) {
	tests := []struct {
		name   string
		parent Role
		child  Role
		want   bool
	}{
		{"superadmin manages admin", RoleSuperAdmin, RoleAdmin, true},
		{"superadmin manages client", RoleSuperAdmin, RoleClient, true},
		{"admin manages master", RoleAdmin, RoleMaster, true},
		{"admin manages client", RoleAdmin, RoleClient, true},
		{"master manages agent", RoleMaster, RoleAgent, true},
		{"agent manages client", RoleAgent, RoleClient, true},
		{"admin cannot manage superadmin", RoleAdmin, RoleSuperAdmin, false},
		{"client cannot manage anyone", RoleClient, RoleAgent, false},
		{"same role cannot manage", RoleAdmin, RoleAdmin, false},
		{"agent cannot manage master", RoleAgent, RoleMaster, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.parent.CanManage(tt.child); got != tt.want {
				t.Errorf("CanManage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_AvailableBalance(t *testing.T) {
	tests := []struct {
		name     string
		balance  float64
		exposure float64
		want     float64
	}{
		{"no exposure", 1000, 0, 1000},
		{"partial exposure", 1000, 300, 700},
		{"full exposure", 1000, 1000, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Balance: tt.balance, Exposure: tt.exposure}
			if got := u.AvailableBalance(); got != tt.want {
				t.Errorf("AvailableBalance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_CanPlaceBet(t *testing.T) {
	tests := []struct {
		name    string
		user    User
		stake   float64
		wantErr bool
	}{
		{
			name:    "client with sufficient balance",
			user:    User{Role: RoleClient, Status: "active", Balance: 1000, Exposure: 0},
			stake:   500,
			wantErr: false,
		},
		{
			name:    "non-client cannot bet",
			user:    User{Role: RoleAdmin, Status: "active", Balance: 1000, Exposure: 0},
			stake:   100,
			wantErr: true,
		},
		{
			name:    "suspended user",
			user:    User{Role: RoleClient, Status: "suspended", Balance: 1000, Exposure: 0},
			stake:   100,
			wantErr: true,
		},
		{
			name:    "insufficient balance",
			user:    User{Role: RoleClient, Status: "active", Balance: 100, Exposure: 50},
			stake:   100,
			wantErr: true,
		},
		{
			name:    "exact available balance",
			user:    User{Role: RoleClient, Status: "active", Balance: 100, Exposure: 0},
			stake:   100,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.CanPlaceBet(tt.stake)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanPlaceBet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
