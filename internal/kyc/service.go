package kyc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"regexp"
	"strings"
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
	RejectionReason string     `json:"rejection_reason,omitempty"`
}

type SubmitKYCRequest struct {
	FullName     string `json:"full_name" validate:"required,min=2,max=100"`
	DateOfBirth  string `json:"date_of_birth" validate:"required"`
	Phone        string `json:"phone" validate:"required,min=10,max=15"`
	DocumentType string `json:"document_type" validate:"required,oneof=aadhaar pan passport driving_license"`
	DocumentID   string `json:"document_id" validate:"required,min=5,max=50"`
}

var (
	aadhaarRegex  = regexp.MustCompile(`^\d{12}$`)
	panRegex      = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]{1}$`)
	passportRegex = regexp.MustCompile(`^[A-Za-z0-9]{6,9}$`)

	kycEncryptionKey []byte
)

func init() {
	secret := os.Getenv("KYC_ENCRYPTION_SECRET")
	if secret == "" {
		secret = os.Getenv("ENCRYPTION_SECRET")
	}
	if secret == "" {
		log.Fatal("FATAL: KYC_ENCRYPTION_SECRET or ENCRYPTION_SECRET environment variable must be set")
	}
	hash := sha256.Sum256([]byte(secret))
	kycEncryptionKey = hash[:]
}

func encryptDocumentID(plaintext string) (string, error) {
	block, err := aes.NewCipher(kycEncryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptDocumentID(encrypted string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(kycEncryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// maskDocumentID masks all but the last 4 characters of a document ID.
func maskDocumentID(docID string) string {
	if len(docID) <= 4 {
		return docID
	}
	return strings.Repeat("*", len(docID)-4) + docID[len(docID)-4:]
}

// validateDocumentID validates the format of the document ID based on its type.
func validateDocumentID(docType, docID string) error {
	switch docType {
	case "aadhaar":
		if !aadhaarRegex.MatchString(docID) {
			return fmt.Errorf("invalid Aadhaar number: must be exactly 12 digits")
		}
	case "pan":
		if !panRegex.MatchString(docID) {
			return fmt.Errorf("invalid PAN: must be 5 uppercase letters, 4 digits, 1 uppercase letter (e.g., ABCDE1234F)")
		}
	case "passport":
		if !passportRegex.MatchString(docID) {
			return fmt.Errorf("invalid passport number: must be 6-9 alphanumeric characters")
		}
	case "driving_license":
		if len(docID) < 5 || len(docID) > 20 {
			return fmt.Errorf("invalid driving license number: must be 5-20 characters")
		}
	default:
		return fmt.Errorf("unsupported document type: %s", docType)
	}
	return nil
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

	// Decrypt and mask document ID in API response
	if profile.DocumentID != "" {
		decrypted, err := decryptDocumentID(profile.DocumentID)
		if err == nil {
			profile.DocumentID = maskDocumentID(decrypted)
		} else {
			// If decryption fails, it may be legacy plaintext — mask it directly
			profile.DocumentID = maskDocumentID(profile.DocumentID)
		}
	}

	return profile, nil
}

func (s *Service) SubmitKYC(ctx context.Context, userID int64, req *SubmitKYCRequest) (*KYCProfile, error) {
	// Validate document ID format
	if err := validateDocumentID(req.DocumentType, req.DocumentID); err != nil {
		return nil, err
	}

	// Encrypt document ID before storing
	encryptedDocID, err := encryptDocumentID(req.DocumentID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to encrypt document ID", "error", err)
		return nil, fmt.Errorf("failed to process document")
	}

	// Atomic check-and-update: only allow submission if status is not already verified or under_review
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET full_name = $1, date_of_birth = $2::date, phone = $3,
		        kyc_document_type = $4, kyc_document_id = $5, kyc_status = 'under_review',
		        updated_at = NOW()
		 WHERE id = $6 AND COALESCE(kyc_status, 'pending') NOT IN ('verified', 'under_review')`,
		req.FullName, req.DateOfBirth, req.Phone,
		req.DocumentType, encryptedDocID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("submit kyc: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("submit kyc rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("KYC already verified or under review")
	}

	s.logger.InfoContext(ctx, "KYC submitted", "user_id", userID, "doc_type", req.DocumentType)
	return s.GetKYCStatus(ctx, userID)
}

func (s *Service) ApproveKYC(ctx context.Context, userID int64, adminID int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET kyc_status = 'verified', kyc_verified_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND kyc_status = 'under_review'`, userID,
	)
	if err != nil {
		return fmt.Errorf("approve kyc: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("approve kyc rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("KYC is not under review for user %d", userID)
	}

	s.logger.InfoContext(ctx, "KYC approved", "user_id", userID, "admin_id", adminID)
	return nil
}

func (s *Service) RejectKYC(ctx context.Context, userID int64, adminID int64, reason string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE users SET kyc_status = 'rejected', kyc_rejection_reason = $1, updated_at = NOW()
		 WHERE id = $2 AND kyc_status = 'under_review'`, reason, userID,
	)
	if err != nil {
		return fmt.Errorf("reject kyc: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reject kyc rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("KYC is not under review for user %d", userID)
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
		// Decrypt and mask document ID
		if p.DocumentID != "" {
			decrypted, err := decryptDocumentID(p.DocumentID)
			if err == nil {
				p.DocumentID = maskDocumentID(decrypted)
			} else {
				p.DocumentID = maskDocumentID(p.DocumentID)
			}
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
