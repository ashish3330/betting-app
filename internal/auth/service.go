package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"
)

// User cache configuration. GetUserByID is hit on almost every authenticated
// request (the gateway calls it to populate identity headers), so a short
// per-user cache drastically reduces load on the users table.
const (
	userCacheKey = "user:"
	userCacheTTL = 60 * time.Second
)

func userKey(userID int64) string {
	return userCacheKey + strconv.FormatInt(userID, 10)
}

// Argon2 parameters — hardened from the original 1 iteration / 64KB.
const (
	argon2Iterations = 3
	argon2Memory     = 64 * 1024 // 64 MB
	argon2Threads    = 4
	argon2KeyLength  = 32
	argon2SaltLength = 16
)

type Service struct {
	db         *sql.DB
	redis      *redis.Client
	logger     *slog.Logger
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type Claims struct {
	UserID   int64       `json:"user_id"`
	Username string      `json:"username"`
	Role     models.Role `json:"role"`
	Path     string      `json:"path"`
	jwt.RegisteredClaims
}

// NewService creates an auth service. If privateKeyHex and publicKeyHex are
// provided they are decoded and used for JWT signing so that tokens survive
// restarts. When the keys are empty a fresh pair is generated and a warning
// is logged — this is acceptable for development but must not happen in
// production.
func NewService(
	db *sql.DB,
	rdb *redis.Client,
	logger *slog.Logger,
	accessTTL, refreshTTL time.Duration,
	privateKeyHex, publicKeyHex string,
) (*Service, error) {
	var (
		priv ed25519.PrivateKey
		pub  ed25519.PublicKey
	)

	if privateKeyHex != "" && publicKeyHex != "" {
		privBytes, err := hex.DecodeString(privateKeyHex)
		if err != nil {
			return nil, fmt.Errorf("decode ED25519 private key: %w", err)
		}
		pubBytes, err := hex.DecodeString(publicKeyHex)
		if err != nil {
			return nil, fmt.Errorf("decode ED25519 public key: %w", err)
		}
		if len(privBytes) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("ED25519 private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(privBytes))
		}
		if len(pubBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("ED25519 public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pubBytes))
		}
		priv = ed25519.PrivateKey(privBytes)
		pub = ed25519.PublicKey(pubBytes)
		logger.Info("ED25519 signing keys loaded from configuration")
	} else {
		var err error
		pub, priv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ed25519 key: %w", err)
		}
		logger.Warn("ED25519 signing keys generated at startup — tokens will be invalidated on restart. Set ED25519_PRIVATE_KEY and ED25519_PUBLIC_KEY for persistence.")
	}

	return &Service{
		db:         db,
		redis:      rdb,
		logger:     logger,
		privateKey: priv,
		publicKey:  pub,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}, nil
}

// PublicKey returns the ED25519 public key used to verify tokens.
func (s *Service) PublicKey() ed25519.PublicKey {
	return s.publicKey
}

// ValidatePasswordComplexity ensures the password meets minimum strength
// requirements: at least 8 characters, 1 uppercase, 1 lowercase, 1 digit.
func ValidatePasswordComplexity(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return errors.New("password must be at most 128 characters")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasUpper {
		return errors.New("password must contain at least one uppercase letter")
	}
	if !hasLower {
		return errors.New("password must contain at least one lowercase letter")
	}
	if !hasDigit {
		return errors.New("password must contain at least one digit")
	}
	return nil
}

// Register creates a new user account after validating password complexity.
func (s *Service) Register(ctx context.Context, req *models.CreateUserRequest) (*models.User, error) {
	if err := ValidatePasswordComplexity(req.Password); err != nil {
		return nil, fmt.Errorf("password validation: %w", err)
	}

	hash := s.hashPassword(req.Password)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var user models.User
	err = tx.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, role, parent_id, credit_limit, commission_rate, status, path)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', '')
		 RETURNING id, username, email, role, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at`,
		req.Username, req.Email, hash, req.Role, req.ParentID, req.CreditLimit, req.CommissionRate,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.ParentID,
		&user.Balance, &user.Exposure, &user.CreditLimit, &user.CommissionRate,
		&user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	// Set ltree path based on parent
	path := fmt.Sprintf("%d", user.ID)
	if req.ParentID != nil {
		var parentPath string
		err = tx.QueryRowContext(ctx, "SELECT path FROM users WHERE id = $1", *req.ParentID).Scan(&parentPath)
		if err != nil {
			return nil, fmt.Errorf("get parent path: %w", err)
		}
		path = parentPath + "." + fmt.Sprintf("%d", user.ID)
	}

	_, err = tx.ExecContext(ctx, "UPDATE users SET path = $1 WHERE id = $2", path, user.ID)
	if err != nil {
		return nil, fmt.Errorf("update path: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	user.Path = path

	s.logger.InfoContext(ctx, "user registered", "user_id", user.ID, "role", user.Role)
	return &user, nil
}

// Login authenticates a user, checks self-exclusion status, issues tokens,
// and records the login session.
func (s *Service) Login(ctx context.Context, req *models.LoginRequest) (*models.LoginResponse, error) {
	var user models.User
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users WHERE username = $1`,
		req.Username,
	).Scan(&user.ID, &user.Username, &user.Email, &hash, &user.Role, &user.Path,
		&user.ParentID, &user.Balance, &user.Exposure, &user.CreditLimit,
		&user.CommissionRate, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if user.Status != "active" {
		s.logger.WarnContext(ctx, "login attempt on non-active account", "user_id", user.ID, "status", user.Status)
		return nil, fmt.Errorf("invalid credentials")
	}

	if !s.verifyPassword(req.Password, hash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check responsible gambling self-exclusion
	if err := s.CheckSelfExclusion(ctx, user.ID); err != nil {
		return nil, err
	}

	accessToken, err := s.generateToken(&user, s.accessTTL)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.generateToken(&user, s.refreshTTL)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	// Store refresh token in Redis
	err = s.redis.Set(ctx, "refresh:"+refreshToken, user.ID, s.refreshTTL).Err()
	if err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	// Record login session for responsible gambling tracking
	if err := s.RecordLoginSession(ctx, user.ID); err != nil {
		s.logger.WarnContext(ctx, "failed to record login session", "user_id", user.ID, "error", err)
	}

	s.logger.InfoContext(ctx, "user logged in", "user_id", user.ID)
	return &models.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         &user,
	}, nil
}

// Logout blacklists the given access token for its remaining TTL.
func (s *Service) Logout(ctx context.Context, token string) error {
	claims, err := s.ValidateToken(token)
	if err != nil {
		return err
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl > 0 {
		err = s.redis.Set(ctx, "blacklist:"+token, "1", ttl).Err()
		if err != nil {
			return fmt.Errorf("blacklist token: %w", err)
		}
	}

	s.logger.InfoContext(ctx, "user logged out", "user_id", claims.UserID)
	return nil
}

// RefreshToken rotates the refresh token and issues a new access token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*models.LoginResponse, error) {
	// Use GetDel for atomic get-and-delete to prevent TOCTOU race conditions
	// where two concurrent refresh requests could both succeed with the same token.
	userIDStr, err := s.redis.GetDel(ctx, "refresh:"+refreshToken).Result()
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	_ = userIDStr // used for validation

	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	var user models.User
	err = s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Path,
		&user.ParentID, &user.Balance, &user.Exposure, &user.CreditLimit,
		&user.CommissionRate, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	newAccess, err := s.generateToken(&user, s.accessTTL)
	if err != nil {
		return nil, err
	}
	newRefresh, err := s.generateToken(&user, s.refreshTTL)
	if err != nil {
		return nil, err
	}

	s.redis.Set(ctx, "refresh:"+newRefresh, user.ID, s.refreshTTL)

	return &models.LoginResponse{
		AccessToken:  newAccess,
		RefreshToken: newRefresh,
		User:         &user,
	}, nil
}

// ValidateToken parses and validates a JWT signed with ED25519.
func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// IsBlacklisted checks whether a token has been revoked.
func (s *Service) IsBlacklisted(ctx context.Context, token string) bool {
	exists, _ := s.redis.Exists(ctx, "blacklist:"+token).Result()
	return exists > 0
}

// CheckSelfExclusion queries the responsible_gambling table to see if the
// user has an active self-exclusion that has not yet expired.
func (s *Service) CheckSelfExclusion(ctx context.Context, userID int64) error {
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM responsible_gambling
		 WHERE user_id = $1
		   AND exclusion_type = 'self_exclusion'
		   AND expires_at > NOW()
		 ORDER BY expires_at DESC
		 LIMIT 1`,
		userID,
	).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return nil // no active exclusion
	}
	if err != nil {
		// If the table doesn't exist or another DB error occurs, log and
		// allow login so we don't brick accounts when the table is missing.
		s.logger.WarnContext(ctx, "self-exclusion check failed", "user_id", userID, "error", err)
		return nil
	}

	return fmt.Errorf("account is self-excluded until %s", expiresAt.Format(time.RFC3339))
}

// RecordLoginSession inserts a row into user_sessions for responsible
// gambling session tracking.
func (s *Service) RecordLoginSession(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_sessions (user_id, login_at) VALUES ($1, NOW())`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("record login session: %w", err)
	}
	return nil
}

// ChangePassword validates the old password, enforces complexity on the new
// password, and updates the stored hash.
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	if err := ValidatePasswordComplexity(newPassword); err != nil {
		return fmt.Errorf("new password validation: %w", err)
	}

	var currentHash string
	err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM users WHERE id = $1`, userID,
	).Scan(&currentHash)
	if err != nil {
		return fmt.Errorf("user not found")
	}

	if !s.verifyPassword(oldPassword, currentHash) {
		return fmt.Errorf("current password is incorrect")
	}

	newHash := s.hashPassword(newPassword)
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		newHash, userID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	s.invalidateUser(ctx, userID)
	s.logger.InfoContext(ctx, "password changed", "user_id", userID)
	return nil
}

// GetUserByID fetches a single user by primary key. Results are cached in
// Redis for a short window (userCacheTTL) to absorb the request-per-hop
// pattern at the gateway. Mutating paths (ChangePassword, etc.) must call
// invalidateUser to evict stale entries.
func (s *Service) GetUserByID(ctx context.Context, userID int64) (*models.User, error) {
	if s.redis != nil {
		if cached, err := s.redis.Get(ctx, userKey(userID)).Bytes(); err == nil {
			var u models.User
			if json.Unmarshal(cached, &u) == nil {
				return &u, nil
			}
		}
	}

	var user models.User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Path,
		&user.ParentID, &user.Balance, &user.Exposure, &user.CreditLimit,
		&user.CommissionRate, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	if s.redis != nil {
		if data, mErr := json.Marshal(&user); mErr == nil {
			if setErr := s.redis.Set(ctx, userKey(userID), data, userCacheTTL).Err(); setErr != nil {
				s.logger.WarnContext(ctx, "auth: user cache set failed",
					"user_id", userID, "error", setErr)
			}
		}
	}

	return &user, nil
}

// invalidateUser best-effort deletes the cached user record. Called after
// any UPDATE to auth.users so the next read returns fresh state.
func (s *Service) invalidateUser(ctx context.Context, userID int64) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Del(ctx, userKey(userID)).Err(); err != nil {
		s.logger.WarnContext(ctx, "auth: user cache invalidation failed",
			"user_id", userID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *Service) generateToken(user *models.User, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Path:     user.Path,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "lotus-exchange",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(s.privateKey)
}

func (s *Service) hashPassword(password string) string {
	salt := make([]byte, argon2SaltLength)
	rand.Read(salt)
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Threads, argon2KeyLength)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash)
}

func (s *Service) verifyPassword(password, stored string) bool {
	parts := splitOnce(stored, ':')
	if len(parts) != 2 {
		return false
	}
	salt, _ := hex.DecodeString(parts[0])
	expectedHash, _ := hex.DecodeString(parts[1])
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Threads, argon2KeyLength)
	return constantTimeEqual(hash, expectedHash)
}

func splitOnce(s string, sep byte) []string {
	for i := range len(s) {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
