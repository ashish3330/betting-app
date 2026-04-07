package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	mrand "math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// ─── In-memory store (replaces Postgres + Redis) ────────────────────────────

type Store struct {
	mu sync.RWMutex // primary lock for all data

	users          map[int64]*User
	usersByName    map[string]*User
	nextUserID     atomic.Int64
	ledger         []*LedgerEntry
	nextLedgerID   atomic.Int64
	bets           map[string]*Bet
	markets        map[string]*Market
	runners        map[string][]*Runner // market_id -> runners
	orderBooks     map[string][]*Order  // key "market_id:side" -> orders
	sports         []*Sport
	competitions   []*Competition
	events         []*Event
	casinoGames    []*CasinoGame
	casinoSessions map[string]*CasinoSession
	paymentTxns    map[string]*PaymentTx
	notifications  []*Notification
	fraudAlerts    []*FraudAlert
	settlementEvts []*SettlementEvent
	liveScores     map[string]*LiveScoreData

	// Platform revenue tracking
	platformRevenue struct {
		TotalCommission    float64 `json:"total_commission"`     // From exchange market winnings
		TotalBookmakerPnL  float64 `json:"total_bookmaker_pnl"`  // House profit from bookmaker/fancy (user loss = our profit)
		TotalCasinoRevenue float64 `json:"total_casino_revenue"` // Casino house edge revenue
	}

	// Referral system: code -> userID
	referralCodes map[string]int64

	// Responsible gambling limits: userID -> limits
	responsibleLimits map[int64]*ResponsibleGamblingLimits

	// JWT keys
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey

	// Blacklisted tokens
	blacklist map[string]time.Time
	// Refresh tokens: token -> userID
	refreshTokens map[string]int64

	// OTP store: userID -> OTP entry
	otpStore map[int64]*OTPEntry

	// CSRF tokens: token -> userID
	csrfTokens     map[string]int64
	csrfTokenTimes map[string]time.Time // token -> creation time

	// Refresh token creation times for expiry tracking
	refreshTokenTimes map[string]time.Time // token -> creation time

	// Brute force protection: username -> login attempt tracking
	loginAttempts map[string]*LoginAttempt

	// Audit log
	auditLog    []*AuditEntry
	nextAuditID atomic.Int64

	// Login history
	loginHistory []*LoginRecord
}

// ─── Models ─────────────────────────────────────────────────────────────────

type User struct {
	ID             int64   `json:"id"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	PasswordHash   string  `json:"-"`
	Role           string  `json:"role"`
	Path           string  `json:"path"`
	ParentID       *int64  `json:"parent_id,omitempty"`
	Balance        float64 `json:"balance"`
	Exposure       float64 `json:"exposure"`
	CreditLimit    float64 `json:"credit_limit"`
	CommissionRate float64 `json:"commission_rate"`
	Status         string  `json:"status"`
	ReferralCode   string  `json:"referral_code,omitempty"`
	ReferredBy     int64   `json:"referred_by,omitempty"`
	OTPSecret      string  `json:"-"`
	OTPEnabled     bool    `json:"otp_enabled"`
	IsDemo         bool    `json:"is_demo,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func (u *User) Available() float64 { return u.Balance - u.Exposure }

type LedgerEntry struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"user_id"`
	Amount    float64 `json:"amount"`
	Type      string  `json:"type"`
	Reference string  `json:"reference"`
	BetID     string  `json:"bet_id,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type Bet struct {
	ID             string  `json:"id"`
	MarketID       string  `json:"market_id"`
	SelectionID    int64   `json:"selection_id"`
	UserID         int64   `json:"user_id"`
	Side           string  `json:"side"`
	Price          float64 `json:"price"`
	Stake          float64 `json:"stake"`
	MatchedStake   float64 `json:"matched_stake"`
	UnmatchedStake float64 `json:"unmatched_stake"`
	Profit         float64 `json:"profit"`
	Status         string  `json:"status"`
	ClientRef      string  `json:"client_ref"`
	CreatedAt      string  `json:"created_at"`
}

type Market struct {
	ID           string  `json:"id"`
	EventID      string  `json:"event_id"`
	Sport        string  `json:"sport"`
	Name         string  `json:"name"`
	MarketType   string  `json:"market_type"`
	Status       string  `json:"status"`
	InPlay       bool    `json:"in_play"`
	StartTime    string  `json:"start_time"`
	TotalMatched float64 `json:"total_matched"`
}

type Runner struct {
	MarketID    string      `json:"-"`
	SelectionID int64       `json:"selection_id"`
	Name        string      `json:"name"`
	Status      string      `json:"status"`
	BackPrices  []PriceSize `json:"back_prices"`
	LayPrices   []PriceSize `json:"lay_prices"`
	RunValue    float64     `json:"run_value,omitempty"`
	YesRate     float64     `json:"yes_rate,omitempty"`
	NoRate      float64     `json:"no_rate,omitempty"`
}

type LiveScoreData struct {
	EventID      string `json:"event_id"`
	Home         string `json:"home"`
	Away         string `json:"away"`
	HomeScore    string `json:"home_score"`
	AwayScore    string `json:"away_score"`
	Overs        string `json:"overs,omitempty"`
	RunRate      string `json:"run_rate,omitempty"`
	RequiredRate string `json:"required_rate,omitempty"`
	LastWicket   string `json:"last_wicket,omitempty"`
	Partnership  string `json:"partnership,omitempty"`
}

type PriceSize struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type Order struct {
	ID        string  `json:"id"`
	MarketID  string  `json:"market_id"`
	UserID    int64   `json:"user_id"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Remaining float64 `json:"remaining"`
	Timestamp int64   `json:"timestamp"`
}

type Sport struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
}

type Competition struct {
	ID         string `json:"id"`
	SportID    string `json:"sport_id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	Status     string `json:"status"`
	MatchCount int    `json:"match_count"`
}

type Event struct {
	ID            string `json:"id"`
	CompetitionID string `json:"competition_id"`
	SportID       string `json:"sport_id"`
	Name          string `json:"name"`
	HomeTeam      string `json:"home_team"`
	AwayTeam      string `json:"away_team"`
	StartTime     string `json:"start_time"`
	Status        string `json:"status"`
	InPlay        bool   `json:"in_play"`
}

type CasinoGame struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Provider  string   `json:"provider"`
	MinBet    float64  `json:"min_bet"`
	MaxBet    float64  `json:"max_bet"`
	Active    bool     `json:"active"`
	Thumbnail string   `json:"thumbnail"`
	StreamURL string   `json:"stream_url"`
	IframeURL string   `json:"iframe_url"`
	RTP       float64  `json:"rtp"`
	Tags      []string `json:"tags"`
	Popular   bool     `json:"popular"`
	New       bool     `json:"new"`
}

type CasinoSession struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	GameType   string `json:"game_type"`
	ProviderID string `json:"provider_id"`
	Status     string `json:"status"`
	StreamURL  string `json:"stream_url"`
	Token      string `json:"token"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
}

type PaymentTx struct {
	ID        string  `json:"id"`
	UserID    int64   `json:"user_id"`
	Direction string  `json:"direction"`
	Method    string  `json:"method"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Status    string  `json:"status"`
	UPIID     string  `json:"upi_id,omitempty"`
	Wallet    string  `json:"wallet_address,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type Notification struct {
	ID      string `json:"id"`
	UserID  int64  `json:"user_id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Read    bool   `json:"read"`
	Created string `json:"created_at"`
}

// ─── Notification helpers ────────────────────────────────────────────────────

var nextNotifID atomic.Int64

func (s *Store) AddNotification(userID int64, typ, title, message string) {
	id := nextNotifID.Add(1)
	nid := fmt.Sprintf("notif-%d", id)
	n := &Notification{
		ID:      nid,
		UserID:  userID,
		Type:    typ,
		Title:   title,
		Message: message,
		Read:    false,
		Created: time.Now().Format(time.RFC3339),
	}
	s.mu.Lock()
	s.notifications = append(s.notifications, n)
	s.mu.Unlock()
	if useDB() {
		dbAddNotification(nid, userID, typ, title, message)
	}
}

// NotifyHierarchy sends a notification to all parents up the chain (agent → master → admin → superadmin).
// Collects all target IDs under the read lock, then sends notifications outside the lock
// to avoid holding the lock during potentially slow notification writes.
func (s *Store) NotifyHierarchy(childID int64, typ, title, message string) {
	// Phase 1: collect all parent IDs under the read lock
	parentIDs := s.collectHierarchyIDs(childID)

	// Phase 2: send notifications outside the lock
	for _, pid := range parentIDs {
		s.AddNotification(pid, typ, title, message)
	}
}

// collectHierarchyIDs returns parent + superadmin user IDs for the given child, under a read lock.
func (s *Store) collectHierarchyIDs(childID int64) []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	child := s.users[childID]
	if child == nil {
		return nil
	}
	// Walk up the parent chain
	var parentIDs []int64
	current := child
	for current.ParentID != nil {
		parent, ok := s.users[*current.ParentID]
		if !ok {
			break
		}
		parentIDs = append(parentIDs, parent.ID)
		current = parent
	}
	// Also notify all superadmins
	for _, u := range s.users {
		if u.Role == "superadmin" {
			found := false
			for _, pid := range parentIDs {
				if pid == u.ID {
					found = true
					break
				}
			}
			if !found {
				parentIDs = append(parentIDs, u.ID)
			}
		}
	}
	return parentIDs
}

func (s *Store) GetNotifications(userID int64, unreadOnly bool, limit, offset int) []*Notification {
	if useDB() {
		return dbGetNotifications(userID, unreadOnly, limit, offset)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*Notification
	// Reverse order (newest first)
	for i := len(s.notifications) - 1; i >= 0; i-- {
		n := s.notifications[i]
		if n.UserID != userID {
			continue
		}
		if unreadOnly && n.Read {
			continue
		}
		out = append(out, n)
	}
	if offset >= len(out) {
		return []*Notification{}
	}
	out = out[offset:]
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

func (s *Store) GetUnreadCount(userID int64) int {
	if useDB() {
		return dbGetUnreadCount(userID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, n := range s.notifications {
		if n.UserID == userID && !n.Read {
			count++
		}
	}
	return count
}

func (s *Store) MarkNotificationRead(userID int64, notifID string) bool {
	if useDB() {
		return dbMarkNotificationRead(userID, notifID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.notifications {
		if n.ID == notifID && n.UserID == userID {
			n.Read = true
			return true
		}
	}
	return false
}

func (s *Store) MarkAllNotificationsRead(userID int64) int {
	if useDB() {
		return dbMarkAllNotificationsRead(userID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, n := range s.notifications {
		if n.UserID == userID && !n.Read {
			n.Read = true
			count++
		}
	}
	return count
}

type FraudAlert struct {
	ID        string  `json:"id"`
	UserID    int64   `json:"user_id"`
	Type      string  `json:"type"`
	Risk      string  `json:"risk_level"`
	Details   string  `json:"details"`
	Score     float64 `json:"score"`
	Resolved  bool    `json:"resolved"`
	CreatedAt string  `json:"created_at"`
}

type SettlementEvent struct {
	ID        int64   `json:"id"`
	MarketID  string  `json:"market_id"`
	BetID     string  `json:"bet_id"`
	UserID    int64   `json:"user_id"`
	EventType string  `json:"event_type"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
}

// ─── Security types ─────────────────────────────────────────────────────────

type OTPEntry struct {
	Code   string
	Expiry time.Time
}

type LoginAttempt struct {
	Count       int
	LastTry     time.Time
	LockedUntil time.Time
}

type AuditEntry struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Details   string `json:"details"`
	IP        string `json:"ip"`
	Timestamp string `json:"timestamp"`
}

type LoginRecord struct {
	UserID    int64  `json:"user_id"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	LoginAt   string `json:"login_at"`
	Success   bool   `json:"success"`
}

type ResponsibleGamblingLimits struct {
	DailyDeposit   float64 `json:"daily_deposit_limit"`
	DailyLoss      float64 `json:"daily_loss_limit"`
	MaxStake       float64 `json:"max_stake_per_bet"`
	SessionMinutes int     `json:"session_limit_minutes"`
	SelfExcluded   bool    `json:"self_excluded"`
	ExcludedUntil  string  `json:"excluded_until,omitempty"`
}

// ─── Constructor ────────────────────────────────────────────────────────────

func NewStore() *Store {
	var priv ed25519.PrivateKey
	var pub ed25519.PublicKey

	// Load persistent keys from env (survive restarts)
	if privHex := os.Getenv("ED25519_PRIVATE_KEY"); privHex != "" {
		privBytes, err := hex.DecodeString(privHex)
		if err == nil && len(privBytes) == ed25519.PrivateKeySize {
			priv = ed25519.PrivateKey(privBytes)
			pub = priv.Public().(ed25519.PublicKey)
		}
	}
	// Generate new keys if not loaded from env
	if priv == nil {
		pub, priv, _ = ed25519.GenerateKey(rand.Reader)
		// Log the generated keys so they can be saved for next restart
		fmt.Fprintf(os.Stderr, "\n⚠  No ED25519_PRIVATE_KEY env set. Generated new keys.\n")
		fmt.Fprintf(os.Stderr, "   To persist tokens across restarts, set:\n")
		fmt.Fprintf(os.Stderr, "   ED25519_PRIVATE_KEY=%s\n\n", hex.EncodeToString(priv))
	}
	s := &Store{
		users:          make(map[int64]*User),
		usersByName:    make(map[string]*User),
		bets:           make(map[string]*Bet),
		markets:        make(map[string]*Market),
		runners:        make(map[string][]*Runner),
		orderBooks:     make(map[string][]*Order),
		casinoSessions: make(map[string]*CasinoSession),
		paymentTxns:    make(map[string]*PaymentTx),
		liveScores:     make(map[string]*LiveScoreData),
		referralCodes:     make(map[string]int64),
		responsibleLimits: make(map[int64]*ResponsibleGamblingLimits),
		blacklist:         make(map[string]time.Time),
		refreshTokens:  make(map[string]int64),
		otpStore:       make(map[int64]*OTPEntry),
		csrfTokens:        make(map[string]int64),
		csrfTokenTimes:    make(map[string]time.Time),
		refreshTokenTimes: make(map[string]time.Time),
		loginAttempts:     make(map[string]*LoginAttempt),
		PrivateKey:     priv,
		PublicKey:       pub,
	}
	s.nextUserID.Store(0)
	s.nextLedgerID.Store(0)

	// If DB is connected, load users from PostgreSQL into memory cache
	if useDB() {
		s.loadUsersFromDB()
	}

	s.seedData()

	// Background cleanup: expired blacklist tokens, OTPs, old audit logs
	go s.startCleanupLoop()

	return s
}

// loadUsersFromDB populates in-memory cache from PostgreSQL
func (s *Store) loadUsersFromDB() {
	users := dbAllUsers()
	if users == nil {
		return
	}
	var maxID int64
	for _, u := range users {
		s.users[u.ID] = u
		s.usersByName[u.Username] = u
		if u.ID > maxID {
			maxID = u.ID
		}
	}
	s.nextUserID.Store(maxID)
	logger.Info("loaded users from DB", "count", len(users))
}

func (s *Store) startCleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()

		// Purge expired blacklisted tokens
		for token, exp := range s.blacklist {
			if now.After(exp) {
				delete(s.blacklist, token)
			}
		}
		// Purge expired OTPs
		for uid, entry := range s.otpStore {
			if now.After(entry.Expiry) {
				delete(s.otpStore, uid)
			}
		}
		// Prune audit log to last 10000 entries
		if len(s.auditLog) > 10000 {
			s.auditLog = s.auditLog[len(s.auditLog)-10000:]
		}
		// Prune login history to last 5000
		if len(s.loginHistory) > 5000 {
			s.loginHistory = s.loginHistory[len(s.loginHistory)-5000:]
		}

		// Purge expired CSRF tokens (older than 24 hours)
		for token, created := range s.csrfTokenTimes {
			if now.Sub(created) > 24*time.Hour {
				delete(s.csrfTokens, token)
				delete(s.csrfTokenTimes, token)
			}
		}

		// Purge expired refresh tokens (older than 7 days)
		for token, created := range s.refreshTokenTimes {
			if now.Sub(created) > 7*24*time.Hour {
				delete(s.refreshTokens, token)
				delete(s.refreshTokenTimes, token)
			}
		}

		// Purge stale login attempts older than 30 minutes
		for username, attempt := range s.loginAttempts {
			if now.Sub(attempt.LastTry) > 30*time.Minute {
				delete(s.loginAttempts, username)
			}
		}

		s.mu.Unlock()
	}
}

// ─── Auth helpers ───────────────────────────────────────────────────────────

func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash)
}

func verifyPassword(password, stored string) bool {
	for i := range len(stored) {
		if stored[i] == ':' {
			salt, _ := hex.DecodeString(stored[:i])
			expected, _ := hex.DecodeString(stored[i+1:])
			hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
			if len(hash) != len(expected) {
				return false
			}
			var r byte
			for j := range hash {
				r |= hash[j] ^ expected[j]
			}
			return r == 0
		}
	}
	return false
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// roundMoney rounds to 2 decimal places to prevent float64 accumulation errors
func roundMoney(amount float64) float64 {
	return math.Round(amount*100) / 100
}

// ─── CRUD ───────────────────────────────────────────────────────────────────

func (s *Store) CreateUser(username, email, password, role string, parentID *int64, creditLimit, commRate float64) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usersByName[username]; exists {
		return nil, fmt.Errorf("username already taken")
	}

	id := s.nextUserID.Add(1)
	path := fmt.Sprintf("%d", id)
	if parentID != nil {
		if p, ok := s.users[*parentID]; ok {
			path = p.Path + "." + fmt.Sprintf("%d", id)
		}
	}

	now := time.Now().Format(time.RFC3339)
	// Determine initial balance:
	// - SuperAdmin: credit_limit is platform seed (created from nothing)
	// - Others: credit_limit is transferred from parent's balance
	initialBalance := 0.0
	if role == "superadmin" {
		initialBalance = creditLimit
	} else if parentID != nil && creditLimit > 0 {
		parent, ok := s.users[*parentID]
		if !ok {
			return nil, fmt.Errorf("parent user not found")
		}
		if parent.Available() < creditLimit {
			return nil, fmt.Errorf("parent has insufficient balance (available: %.2f, requested: %.2f)", parent.Available(), creditLimit)
		}
		// Deduct from parent, give to child
		parent.Balance = roundMoney(parent.Balance - creditLimit)
		initialBalance = creditLimit
	}

	// Generate referral code: REF-{username}-{random4}
	refCode := fmt.Sprintf("REF-%s-%s", strings.ToUpper(username), randHex(2))

	u := &User{
		ID: id, Username: username, Email: email,
		PasswordHash: hashPassword(password),
		Role: role, Path: path, ParentID: parentID,
		Balance: initialBalance, Exposure: 0,
		CreditLimit: creditLimit, CommissionRate: commRate,
		Status: "active", ReferralCode: refCode,
		CreatedAt: now, UpdatedAt: now,
	}
	s.users[id] = u
	s.usersByName[username] = u
	s.referralCodes[refCode] = id

	// Persist to DB
	if useDB() {
		dbCreateUser(username, email, u.PasswordHash, role, path, parentID, initialBalance, creditLimit, commRate, false)
		if parentID != nil && creditLimit > 0 {
			// Update parent balance in DB
			if parent, ok := s.users[*parentID]; ok {
				dbUpdateBalance(parent.ID, parent.Balance, parent.Exposure)
			}
		}
	}

	return u, nil
}

// CreateDemoUser creates a temporary demo account with dummy balance.
// No password needed — returns the user directly for auto-login.
func (s *Store) CreateDemoUser() *User {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextUserID.Add(1)
	now := time.Now().Format(time.RFC3339)
	username := fmt.Sprintf("demo_%s", randHex(4))

	u := &User{
		ID: id, Username: username, Email: username + "@demo.3xbet.com",
		PasswordHash: "", // no password for demo
		Role: "client", Path: fmt.Sprintf("%d", id),
		Balance: 100000, Exposure: 0, // ₹1,00,000 demo balance
		CreditLimit: 100000, CommissionRate: 2,
		Status: "active", IsDemo: true,
		ReferralCode: fmt.Sprintf("DEMO-%s", randHex(2)),
		CreatedAt: now, UpdatedAt: now,
	}
	s.users[id] = u
	s.usersByName[username] = u

	// Add initial ledger entry
	s.addLedger(id, 100000, "demo_credit", "demo:initial_balance", "", now)

	// Persist to DB
	if useDB() {
		dbCreateUser(username, u.Email, "", "client", u.Path, nil, 100000, 100000, 2, true)
	}

	return u
}

func (s *Store) GetUserByUsername(username string) *User {
	s.mu.RLock()
	u := s.usersByName[username]
	s.mu.RUnlock()
	if u != nil {
		// Sync balance from DB for freshness
		if useDB() {
			bal, exp := dbSyncUserBalance(u.ID)
			u.Balance = bal
			u.Exposure = exp
		}
		return u
	}
	// Fallback: check DB directly (user created by another instance)
	if useDB() {
		u = dbGetUserByUsername(username)
		if u != nil {
			s.mu.Lock()
			s.users[u.ID] = u
			s.usersByName[u.Username] = u
			s.mu.Unlock()
		}
	}
	return u
}

func (s *Store) GetUser(id int64) *User {
	s.mu.RLock()
	u := s.users[id]
	s.mu.RUnlock()
	if u != nil {
		if useDB() {
			bal, exp := dbSyncUserBalance(u.ID)
			u.Balance = bal
			u.Exposure = exp
		}
		return u
	}
	if useDB() {
		u = dbGetUser(id)
		if u != nil {
			s.mu.Lock()
			s.users[u.ID] = u
			s.usersByName[u.Username] = u
			s.mu.Unlock()
		}
	}
	return u
}

func (s *Store) AllUsers() []*User {
	if useDB() {
		return dbAllUsers()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) TransferCredit(fromID, toID int64, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	from, ok := s.users[fromID]
	if !ok {
		return fmt.Errorf("sender not found")
	}
	to, ok := s.users[toID]
	if !ok {
		return fmt.Errorf("receiver not found")
	}
	if from.Balance < amount {
		return fmt.Errorf("insufficient balance: %.2f available", from.Balance)
	}
	from.Balance = roundMoney(from.Balance - amount)
	to.Balance = roundMoney(to.Balance + amount)

	now := time.Now().Format(time.RFC3339)
	ref := fmt.Sprintf("transfer:%d:%d:%.0f", fromID, toID, amount)
	s.addLedger(fromID, -amount, "transfer", ref+":debit", "", now)
	s.addLedger(toID, amount, "transfer", ref+":credit", "", now)

	if useDB() {
		dbUpdateBalance(fromID, from.Balance, from.Exposure)
		dbUpdateBalance(toID, to.Balance, to.Exposure)
	}
	return nil
}

func (s *Store) addLedger(userID int64, amount float64, typ, ref, betID, ts string) {
	id := s.nextLedgerID.Add(1)
	s.ledger = append(s.ledger, &LedgerEntry{
		ID: id, UserID: userID, Amount: amount,
		Type: typ, Reference: ref, BetID: betID, CreatedAt: ts,
	})
	if useDB() {
		dbAddLedger(userID, amount, typ, ref, betID)
	}
}

func (s *Store) GetLedger(userID int64, limit, offset int) []*LedgerEntry {
	if useDB() {
		return dbGetLedger(userID, limit, offset)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*LedgerEntry
	for i := len(s.ledger) - 1; i >= 0; i-- {
		if s.ledger[i].UserID == userID {
			out = append(out, s.ledger[i])
		}
	}
	if offset > len(out) {
		return nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out
}

func (s *Store) HoldFunds(userID int64, amount float64, betID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if u.Available() < amount {
		return fmt.Errorf("insufficient balance: available %.2f, required %.2f", u.Available(), amount)
	}
	u.Exposure = roundMoney(u.Exposure + amount)
	now := time.Now().Format(time.RFC3339)
	s.addLedger(userID, -amount, "hold", "hold:"+betID, betID, now)
	if useDB() {
		dbUpdateBalance(userID, u.Balance, u.Exposure)
	}
	return nil
}

func (s *Store) ReleaseFunds(userID int64, amount float64, betID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[userID]
	if !ok {
		return
	}
	u.Exposure = math.Max(u.Exposure-amount, 0)
	now := time.Now().Format(time.RFC3339)
	s.addLedger(userID, amount, "release", "release:"+betID, betID, now)
	if useDB() {
		dbUpdateBalance(userID, u.Balance, u.Exposure)
	}
}

func (s *Store) SettleBet(userID int64, betID string, pnl, commission float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[userID]
	if !ok {
		return
	}
	u.Balance = roundMoney(u.Balance + pnl)
	now := time.Now().Format(time.RFC3339)
	s.addLedger(userID, pnl, "settlement", "settlement:"+betID, betID, now)
	if commission > 0 {
		u.Balance = roundMoney(u.Balance - commission)
		s.addLedger(userID, -commission, "commission", "commission:"+betID, betID, now)
	}
}

// ─── Matching Engine ────────────────────────────────────────────────────────

type MatchResult struct {
	BetID          string `json:"bet_id"`
	MatchedStake   float64 `json:"matched_stake"`
	UnmatchedStake float64 `json:"unmatched_stake"`
	Status         string  `json:"status"`
	Fills          []Fill  `json:"fills"`
}

type Fill struct {
	CounterBetID string  `json:"counter_bet_id"`
	Price        float64 `json:"price"`
	Size         float64 `json:"size"`
}

func (s *Store) PlaceAndMatch(userID int64, marketID string, selectionID int64, side string, price, stake float64, clientRef string) (*MatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	betID := "bet-" + randHex(8)
	now := time.Now()

	// House model: every bet is immediately fully matched.
	// The house (operator) takes the other side — no order book, no unmatched/partial.
	status := "matched"

	// Record the bet as fully matched
	s.bets[betID] = &Bet{
		ID: betID, MarketID: marketID, SelectionID: selectionID,
		UserID: userID, Side: side, Price: price, Stake: stake,
		MatchedStake: stake, UnmatchedStake: 0,
		Status: status, ClientRef: clientRef, CreatedAt: now.Format(time.RFC3339),
	}

	// Update market total matched
	if m, ok := s.markets[marketID]; ok {
		m.TotalMatched += stake
	}

	// Persist to DB
	if useDB() {
		dbSaveBet(s.bets[betID])
	}

	return &MatchResult{
		BetID: betID, MatchedStake: stake,
		UnmatchedStake: 0, Status: status, Fills: nil,
	}, nil
}

func (s *Store) CancelOrder(marketID, betID, side string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate bet exists and is in a cancellable state
	bet, ok := s.bets[betID]
	if !ok {
		return fmt.Errorf("bet not found: %s", betID)
	}
	if bet.Status == "settled" || bet.Status == "cancelled" || bet.Status == "void" {
		return fmt.Errorf("cannot cancel bet in '%s' status", bet.Status)
	}
	if bet.MarketID != marketID {
		return fmt.Errorf("bet does not belong to market %s", marketID)
	}

	key := marketID + ":" + side
	orders := s.orderBooks[key]
	for i, o := range orders {
		if o.ID == betID {
			// Release funds for unmatched portion
			if u, uOk := s.users[bet.UserID]; uOk && o.Remaining > 0 {
				var holdAmount float64
				if side == "back" {
					holdAmount = o.Remaining
				} else {
					holdAmount = roundMoney(o.Remaining * (o.Price - 1))
				}
				u.Exposure = roundMoney(math.Max(u.Exposure-holdAmount, 0))
			}
			s.orderBooks[key] = append(orders[:i], orders[i+1:]...)
			if bet.MatchedStake > 0 {
				bet.Status = "partial" // keep matched portion alive
			} else {
				bet.Status = "cancelled"
			}
			bet.UnmatchedStake = 0
			if useDB() {
				dbUpdateBet(bet)
			}
			return nil
		}
	}
	return fmt.Errorf("no resting order found for bet: %s", betID)
}

func (s *Store) GetOrderBook(marketID string) (backs []PriceSize, lays []PriceSize) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Aggregate backs by price
	backAgg := make(map[float64]float64)
	for _, o := range s.orderBooks[marketID+":back"] {
		backAgg[o.Price] += o.Remaining
	}
	for p, sz := range backAgg {
		backs = append(backs, PriceSize{Price: p, Size: sz})
	}
	sort.Slice(backs, func(i, j int) bool { return backs[i].Price > backs[j].Price })

	// Aggregate lays by price
	layAgg := make(map[float64]float64)
	for _, o := range s.orderBooks[marketID+":lay"] {
		layAgg[o.Price] += o.Remaining
	}
	for p, sz := range layAgg {
		lays = append(lays, PriceSize{Price: p, Size: sz})
	}
	sort.Slice(lays, func(i, j int) bool { return lays[i].Price < lays[j].Price })

	if len(backs) > 5 {
		backs = backs[:5]
	}
	if len(lays) > 5 {
		lays = lays[:5]
	}
	return
}

// ─── Settlement ─────────────────────────────────────────────────────────────

func (s *Store) SettleMarket(marketID string, winnerSelectionID int64) (int, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Determine market type (bookmaker/fancy = house is counter-party)
	var marketType string
	if m, ok := s.markets[marketID]; ok {
		m.Status = "settled"
		marketType = m.MarketType
	}

	isHouseMarket := marketType == "bookmaker" || marketType == "fancy" || marketType == "session"

	var settled int
	var paidOut float64

	for _, bet := range s.bets {
		if bet.MarketID != marketID {
			continue
		}
		if bet.Status != "matched" && bet.Status != "partial" {
			continue
		}

		var pnl float64
		won := (bet.SelectionID == winnerSelectionID && bet.Side == "back") ||
			(bet.SelectionID != winnerSelectionID && bet.Side == "lay")

		if won {
			if bet.Side == "back" {
				pnl = roundMoney(bet.MatchedStake * (bet.Price - 1))
			} else {
				pnl = roundMoney(bet.MatchedStake)
			}
		} else {
			if bet.Side == "back" {
				pnl = roundMoney(-bet.MatchedStake)
			} else {
				pnl = roundMoney(-bet.MatchedStake * (bet.Price - 1))
			}
		}

		bet.Profit = pnl
		bet.Status = "settled"

		u := s.users[bet.UserID]
		if u == nil {
			continue
		}

		var commission float64

		if isHouseMarket {
			if pnl > 0 {
				commission = roundMoney(pnl * u.CommissionRate / 100)
			}
			s.platformRevenue.TotalBookmakerPnL = roundMoney(s.platformRevenue.TotalBookmakerPnL + (-pnl + commission))
		} else {
			if pnl > 0 {
				commission = roundMoney(pnl * u.CommissionRate / 100)
			}
			s.platformRevenue.TotalCommission = roundMoney(s.platformRevenue.TotalCommission + commission)
		}

		// Release exposure
		u.Exposure = roundMoney(math.Max(u.Exposure-bet.MatchedStake, 0))
		// Apply P&L to user (after commission deduction)
		u.Balance = roundMoney(u.Balance + pnl - commission)

		now := time.Now().Format(time.RFC3339)
		s.addLedger(bet.UserID, bet.MatchedStake, "release", "release:"+bet.ID, bet.ID, now)
		s.addLedger(bet.UserID, pnl, "settlement", "settlement:"+bet.ID, bet.ID, now)
		if commission > 0 {
			s.addLedger(bet.UserID, -commission, "commission", "commission:"+bet.ID, bet.ID, now)
		}

		// Persist to DB
		if useDB() {
			dbUpdateBet(bet)
			dbUpdateBalance(u.ID, u.Balance, u.Exposure)
		}

		settled++
		if pnl > 0 {
			paidOut += pnl - commission
		}
	}

	// Clean order books for this market
	delete(s.orderBooks, marketID+":back")
	delete(s.orderBooks, marketID+":lay")

	return settled, paidOut
}

func (s *Store) VoidMarket(marketID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.markets[marketID]; ok {
		m.Status = "void"
	}

	var voided int
	for _, bet := range s.bets {
		if bet.MarketID != marketID {
			continue
		}
		if bet.Status == "settled" || bet.Status == "void" || bet.Status == "cancelled" {
			continue
		}
		bet.Status = "void"
		voided++

		if u, ok := s.users[bet.UserID]; ok {
			u.Exposure = math.Max(u.Exposure-bet.Stake, 0)
			now := time.Now().Format(time.RFC3339)
			s.addLedger(bet.UserID, bet.Stake, "release", "void:"+bet.ID, bet.ID, now)
		}
	}

	delete(s.orderBooks, marketID+":back")
	delete(s.orderBooks, marketID+":lay")
	return voided
}

// ─── Casino ─────────────────────────────────────────────────────────────────

func (s *Store) GetGameByID(id string) *CasinoGame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.casinoGames {
		if g.ID == id {
			return g
		}
	}
	return nil
}

func (s *Store) CreateCasinoSession(userID int64, gameType, providerID string) *CasinoSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := "cs-" + randHex(8)
	token := randHex(32)
	now := time.Now()

	sess := &CasinoSession{
		ID: id, UserID: userID, GameType: gameType,
		ProviderID: providerID, Status: "active",
		StreamURL: fmt.Sprintf("https://stream.%s.com/hls/%s/%s/stream.m3u8?token=%s", providerID, gameType, id, token),
		Token:     token,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(4 * time.Hour).Format(time.RFC3339),
	}
	s.casinoSessions[id] = sess
	return sess
}

// ─── Payments ───────────────────────────────────────────────────────────────

func (s *Store) CreatePaymentTx(userID int64, direction, method string, amount float64, currency, upiID, wallet string) *PaymentTx {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := "tx_" + randHex(8)
	tx := &PaymentTx{
		ID: id, UserID: userID, Direction: direction, Method: method,
		Amount: amount, Currency: currency, Status: "pending",
		UPIID: upiID, Wallet: wallet,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	s.paymentTxns[id] = tx
	return tx
}

func (s *Store) GetUserPayments(userID int64) []*PaymentTx {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*PaymentTx
	for _, tx := range s.paymentTxns {
		if tx.UserID == userID {
			out = append(out, tx)
		}
	}
	return out
}

// ─── Children ───────────────────────────────────────────────────────────────

func (s *Store) GetChildren(userID int64) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u := s.users[userID]
	if u == nil {
		return nil
	}
	var out []*User
	for _, child := range s.users {
		if child.ID != userID && len(child.Path) > len(u.Path) && child.Path[:len(u.Path)] == u.Path {
			out = append(out, child)
		}
	}
	return out
}

func (s *Store) GetDirectChildren(userID int64) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*User
	for _, u := range s.users {
		if u.ParentID != nil && *u.ParentID == userID {
			out = append(out, u)
		}
	}
	return out
}

// ─── Downline ───────────────────────────────────────────────────────────────

// GetDownlineUsers returns ALL users in a user's subtree (ltree-style path filtering).
func (s *Store) GetDownlineUsers(userID int64) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u := s.users[userID]
	if u == nil {
		return nil
	}
	prefix := u.Path + "."
	var out []*User
	for _, child := range s.users {
		if child.ID != userID && strings.HasPrefix(child.Path, prefix) {
			out = append(out, child)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetDownlineStats returns aggregate stats for a user's downline tree.
func (s *Store) GetDownlineStats(userID int64) map[string]interface{} {
	downline := s.GetDownlineUsers(userID)

	var totalBalance, totalExposure float64
	byRole := make(map[string]int)
	userIDs := make(map[int64]bool)
	for _, u := range downline {
		totalBalance += u.Balance
		totalExposure += u.Exposure
		byRole[u.Role]++
		userIDs[u.ID] = true
	}

	s.mu.RLock()
	var todayBets int
	today := time.Now().Format("2006-01-02")
	for _, b := range s.bets {
		if userIDs[b.UserID] && strings.HasPrefix(b.CreatedAt, today) {
			todayBets++
		}
	}
	s.mu.RUnlock()

	return map[string]interface{}{
		"total_users":    len(downline),
		"total_balance":  totalBalance,
		"total_exposure": totalExposure,
		"users_by_role":  byRole,
		"today_bets":     todayBets,
	}
}

// IsDirectChild returns true if childID's parent is parentID.
func (s *Store) IsDirectChild(parentID, childID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	child, ok := s.users[childID]
	if !ok {
		return false
	}
	return child.ParentID != nil && *child.ParentID == parentID
}

// ─── Live Scores ────────────────────────────────────────────────────────────

func (s *Store) GetLiveScore(eventID string) *LiveScoreData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveScores[eventID]
}

// ─── Bets query ─────────────────────────────────────────────────────────────

func (s *Store) GetUserBets(userID int64) []*Bet {
	if useDB() {
		return dbGetUserBets(userID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Bet
	for _, b := range s.bets {
		if b.UserID == userID {
			out = append(out, b)
		}
	}
	return out
}

func (s *Store) AllBets() []*Bet {
	if useDB() {
		return dbAllBets()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Bet, 0, len(s.bets))
	for _, b := range s.bets {
		out = append(out, b)
	}
	return out
}

// ─── Referral System ────────────────────────────────────────────────────────

// ApplyReferralCode links a new user to a referrer. Call after CreateUser.
func (s *Store) ApplyReferralCode(userID int64, code string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	referrerID, ok := s.referralCodes[code]
	if !ok {
		return 0, fmt.Errorf("invalid referral code")
	}
	if referrerID == userID {
		return 0, fmt.Errorf("cannot refer yourself")
	}

	u, ok := s.users[userID]
	if !ok {
		return 0, fmt.Errorf("user not found")
	}
	u.ReferredBy = referrerID
	return referrerID, nil
}

// GetReferralStats returns referral info for a user.
func (s *Store) GetReferralStats(userID int64) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u := s.users[userID]
	if u == nil {
		return nil
	}

	type referredUser struct {
		Username string  `json:"username"`
		JoinedAt string  `json:"joined_at"`
		Status   string  `json:"status"`
		Earnings float64 `json:"earnings"`
	}

	var referred []referredUser
	var totalEarnings float64

	for _, other := range s.users {
		if other.ReferredBy == userID {
			// Calculate mock earnings: 1% of a notional first deposit (1000)
			earnings := 10.0 // mock: 1% of 1000
			totalEarnings += earnings
			referred = append(referred, referredUser{
				Username: other.Username,
				JoinedAt: other.CreatedAt,
				Status:   other.Status,
				Earnings: earnings,
			})
		}
	}

	return map[string]interface{}{
		"referral_code":  u.ReferralCode,
		"referral_link":  fmt.Sprintf("https://lotusexchange.com/register?ref=%s", u.ReferralCode),
		"total_referrals": len(referred),
		"total_earnings":  totalEarnings,
		"referred_users":  referred,
	}
}

// GetUserByReferralCode finds a user by their referral code.
func (s *Store) GetUserByReferralCode(code string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uid, ok := s.referralCodes[code]
	if !ok {
		return nil
	}
	return s.users[uid]
}

// ─── OTP System ─────────────────────────────────────────────────────────────

func (s *Store) GenerateOTP(userID int64) string {
	code := fmt.Sprintf("%06d", mrand.Intn(1000000))
	s.mu.Lock()
	s.otpStore[userID] = &OTPEntry{Code: code, Expiry: time.Now().Add(5 * time.Minute)}
	s.mu.Unlock()
	return code
}

func (s *Store) VerifyOTP(userID int64, code string) bool {
	s.mu.RLock()
	entry, ok := s.otpStore[userID]
	s.mu.RUnlock()
	if !ok || time.Now().After(entry.Expiry) || entry.Code != code {
		return false
	}
	s.mu.Lock()
	delete(s.otpStore, userID)
	s.mu.Unlock()
	return true
}

// ─── CSRF Tokens ────────────────────────────────────────────────────────────

func (s *Store) GenerateCSRF(userID int64) string {
	token := randHex(16)
	s.mu.Lock()
	s.csrfTokens[token] = userID
	s.csrfTokenTimes[token] = time.Now()
	s.mu.Unlock()
	return token
}

func (s *Store) ValidateCSRF(token string) bool {
	s.mu.RLock()
	_, ok := s.csrfTokens[token]
	s.mu.RUnlock()
	return ok
}

// ─── Brute Force Protection ─────────────────────────────────────────────────

func (s *Store) CheckLoginAttempt(username string) (bool, time.Time) {
	s.mu.RLock()
	attempt, ok := s.loginAttempts[username]
	s.mu.RUnlock()
	if !ok {
		return true, time.Time{} // allowed
	}
	if !attempt.LockedUntil.IsZero() && time.Now().Before(attempt.LockedUntil) {
		return false, attempt.LockedUntil
	}
	return true, time.Time{}
}

func (s *Store) RecordFailedLogin(username string) (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	attempt, ok := s.loginAttempts[username]
	if !ok {
		attempt = &LoginAttempt{}
		s.loginAttempts[username] = attempt
	}

	// Reset count if last try was more than 15 minutes ago
	if time.Since(attempt.LastTry) > 15*time.Minute {
		attempt.Count = 0
	}

	attempt.Count++
	attempt.LastTry = time.Now()

	if attempt.Count >= 5 {
		attempt.LockedUntil = time.Now().Add(30 * time.Minute)
		return false, attempt.LockedUntil
	}
	return true, time.Time{}
}

func (s *Store) ClearLoginAttempts(username string) {
	s.mu.Lock()
	delete(s.loginAttempts, username)
	s.mu.Unlock()
}

// ─── Audit Log ──────────────────────────────────────────────────────────────

func (s *Store) AddAudit(userID int64, username, action, details, ip string) {
	id := s.nextAuditID.Add(1)
	entry := &AuditEntry{
		ID:        id,
		UserID:    userID,
		Username:  username,
		Action:    action,
		Details:   details,
		IP:        ip,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.mu.Lock()
	s.auditLog = append(s.auditLog, entry)
	s.mu.Unlock()
	if useDB() {
		dbAddAudit(userID, username, action, details, ip)
	}
}

func (s *Store) GetAuditLog(userID int64, role string) []*AuditEntry {
	if useDB() {
		if role == "superadmin" {
			return dbGetAuditLog(500)
		}
		return dbGetAuditLogForUser(userID, 500)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if role == "superadmin" {
		out := make([]*AuditEntry, len(s.auditLog))
		copy(out, s.auditLog)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		if len(out) > 500 {
			out = out[:500]
		}
		return out
	}

	u := s.users[userID]
	if u == nil {
		return nil
	}
	prefix := u.Path + "."
	downlineIDs := map[int64]bool{userID: true}
	for _, child := range s.users {
		if child.ID != userID && strings.HasPrefix(child.Path, prefix) {
			downlineIDs[child.ID] = true
		}
	}

	var out []*AuditEntry
	for i := len(s.auditLog) - 1; i >= 0; i-- {
		if downlineIDs[s.auditLog[i].UserID] {
			out = append(out, s.auditLog[i])
		}
		if len(out) >= 500 {
			break
		}
	}
	return out
}

// ─── Login History ──────────────────────────────────────────────────────────

func (s *Store) RecordLogin(userID int64, ip, userAgent string, success bool) {
	record := &LoginRecord{
		UserID:    userID,
		IP:        ip,
		UserAgent: userAgent,
		LoginAt:   time.Now().Format(time.RFC3339),
		Success:   success,
	}
	s.mu.Lock()
	s.loginHistory = append(s.loginHistory, record)
	s.mu.Unlock()
	if useDB() {
		dbRecordLogin(userID, ip, userAgent, success)
	}
}

func (s *Store) GetLoginHistory(userID int64, limit int) []*LoginRecord {
	if useDB() {
		return dbGetLoginHistory(userID, limit)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*LoginRecord
	for i := len(s.loginHistory) - 1; i >= 0; i-- {
		if s.loginHistory[i].UserID == userID {
			out = append(out, s.loginHistory[i])
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}
