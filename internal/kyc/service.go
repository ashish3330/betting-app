package kyc

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

type KYCStatus string

const (
	KYCPending     KYCStatus = "pending"
	KYCVerified    KYCStatus = "verified"
	KYCRejected    KYCStatus = "rejected"
	KYCUnderReview KYCStatus = "under_review"
)

type KYCProfile struct {
	UserID          int64      `json:"user_id"`
	FullName        string     `json:"full_name"`
	DateOfBirth     *string    `json:"date_of_birth"`
	Phone           string     `json:"phone"`
	Status          KYCStatus  `json:"kyc_status"`
	DocumentType    string     `json:"document_type,omitempty"`
	DocumentID      string     `json:"document_id,omitempty"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
}

type SubmitKYCRequest struct {
	FullName     string `json:"full_name" validate:"required,min=2,max=100"`
	DateOfBirth  string `json:"date_of_birth" validate:"required"`
	Phone        string `json:"phone" validate:"required,min=10,max=15"`
	DocumentType string `json:"document_type" validate:"required,oneof=aadhaar pan passport driving_license"`
	DocumentID   string `json:"document_id" validate:"required,min=5,max=50"`
}

type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewService(db *sql.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

func (s *Service) GetKYCStatus(ctx context.Context, userID int64) (*KYCProfile, error) {
	profile := &KYCProfile{UserID: userID}
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(full_name, ''), COALESCE(phone, ''), COALESCE(kyc_status, 'pending'),
		        COALESCE(kyc_document_type, ''), COALESCE(kyc_document_id, ''),
		        kyc_verified_at, COALESCE(kyc_rejection_reason, ''),
		        date_of_birth::text
		 FROM users WHERE id = $1`, userID,
	).Scan(&profile.FullName, &profile.Phone, &profile.Status,
		&profile.DocumentType, &profile.DocumentID,
		&profile.VerifiedAt, &profile.RejectionReason,
		&profile.DateOfBirth)
	if err != nil {
		return nil, fmt.Errorf("get kyc status: %w", err)
	}
	return profile, nil
}

func (s *Service) SubmitKYC(ctx context.Context, userID int64, req *SubmitKYCRequest) (*KYCProfile, error) {
	// Check current status
	current, err := s.GetKYCStatus(ctx, userID)
	if err != nil {
		return nil, err
	}
	if current.Status == KYCVerified {
		return nil, fmt.Errorf("KYC already verified")
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET full_name = $1, date_of_birth = $2::date, phone = $3,
		        kyc_document_type = $4, kyc_document_id = $5, kyc_status = 'under_review',
		        updated_at = NOW()
		 WHERE id = $6`,
		req.FullName, req.DateOfBirth, req.Phone,
		req.DocumentType, req.DocumentID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("submit kyc: %w", err)
	}

	s.logger.InfoContext(ctx, "KYC submitted", "user_id", userID, "doc_type", req.DocumentType)
	return s.GetKYCStatus(ctx, userID)
}

func (s *Service) ApproveKYC(ctx context.Context, userID int64, adminID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET kyc_status = 'verified', kyc_verified_at = NOW(), updated_at = NOW()
		 WHERE id = $1`, userID,
	)
	if err != nil {
		return fmt.Errorf("approve kyc: %w", err)
	}

	s.logger.InfoContext(ctx, "KYC approved", "user_id", userID, "admin_id", adminID)
	return nil
}

func (s *Service) RejectKYC(ctx context.Context, userID int64, adminID int64, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET kyc_status = 'rejected', kyc_rejection_reason = $1, updated_at = NOW()
		 WHERE id = $2`, reason, userID,
	)
	if err != nil {
		return fmt.Errorf("reject kyc: %w", err)
	}

	s.logger.InfoContext(ctx, "KYC rejected", "user_id", userID, "admin_id", adminID, "reason", reason)
	return nil
}

func (s *Service) GetPendingKYC(ctx context.Context, limit int) ([]KYCProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(full_name, ''), COALESCE(phone, ''), COALESCE(kyc_status, 'pending'),
		        COALESCE(kyc_document_type, ''), COALESCE(kyc_document_id, ''),
		        date_of_birth::text
		 FROM users WHERE kyc_status = 'under_review'
		 ORDER BY updated_at ASC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []KYCProfile
	for rows.Next() {
		var p KYCProfile
		if err := rows.Scan(&p.UserID, &p.FullName, &p.Phone, &p.Status,
			&p.DocumentType, &p.DocumentID, &p.DateOfBirth); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

func (s *Service) RequireKYCForWithdrawal(ctx context.Context, userID int64) error {
	var status string
	err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(kyc_status, 'pending') FROM users WHERE id = $1", userID,
	).Scan(&status)
	if err != nil {
		return fmt.Errorf("check kyc: %w", err)
	}
	if status != string(KYCVerified) {
		return fmt.Errorf("KYC verification required for withdrawals (current status: %s)", status)
	}
	return nil
}
