package models

import (
	"fmt"
	"time"
)

type Role string

const (
	RoleSuperAdmin Role = "superadmin"
	RoleAdmin      Role = "admin"
	RoleMaster     Role = "master"
	RoleAgent      Role = "agent"
	RoleClient     Role = "client"
)

func (r Role) IsValid() bool {
	switch r {
	case RoleSuperAdmin, RoleAdmin, RoleMaster, RoleAgent, RoleClient:
		return true
	}
	return false
}

func (r Role) CanManage(child Role) bool {
	hierarchy := map[Role]int{
		RoleSuperAdmin: 5,
		RoleAdmin:      4,
		RoleMaster:     3,
		RoleAgent:      2,
		RoleClient:     1,
	}
	return hierarchy[r] > hierarchy[child]
}

type User struct {
	ID             int64   `json:"id" db:"id"`
	Username       string  `json:"username" db:"username"`
	Email          string  `json:"email" db:"email"`
	PasswordHash   string  `json:"-" db:"password_hash"`
	Path           string  `json:"path" db:"path"`
	Role           Role    `json:"role" db:"role"`
	ParentID       *int64  `json:"parent_id,omitempty" db:"parent_id"`
	Balance        float64 `json:"balance" db:"balance"`
	Exposure       float64 `json:"exposure" db:"exposure"`
	CreditLimit    float64 `json:"credit_limit" db:"credit_limit"`
	CommissionRate float64 `json:"commission_rate" db:"commission_rate"`
	Status         string  `json:"status" db:"status"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

func (u *User) AvailableBalance() float64 {
	return u.Balance - u.Exposure
}

func (u *User) CanPlaceBet(stake float64) error {
	if u.Role != RoleClient {
		return fmt.Errorf("only clients can place bets")
	}
	if u.Status != "active" {
		return fmt.Errorf("account is %s", u.Status)
	}
	if u.AvailableBalance() < stake {
		return fmt.Errorf("insufficient balance: available %.2f, required %.2f", u.AvailableBalance(), stake)
	}
	return nil
}

type CreateUserRequest struct {
	Username       string  `json:"username" validate:"required,username,min=3,max=30"`
	Email          string  `json:"email" validate:"required,email,max=255"`
	Password       string  `json:"password" validate:"required,min=8,max=128"`
	Role           Role    `json:"role" validate:"required"`
	ParentID       *int64  `json:"parent_id"`
	CreditLimit    float64 `json:"credit_limit" validate:"gte=0"`
	CommissionRate float64 `json:"commission_rate" validate:"gte=0,lte=100"`
}

type LoginRequest struct {
	Username string `json:"username" validate:"required,max=50"`
	Password string `json:"password" validate:"required,max=128"`
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         *User  `json:"user"`
}

type CreditTransferRequest struct {
	FromUserID int64   `json:"from_user_id" validate:"required"`
	ToUserID   int64   `json:"to_user_id" validate:"required"`
	Amount     float64 `json:"amount" validate:"required,gt=0"`
}
