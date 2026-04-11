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
	mu sync.RWMutex // primary lock for in-memory bet/user/market state

	// NEW: dedicated mutexes for independent concerns. Splitting these off
	// from the global s.mu lets authenticated requests (blacklist check) and
	// notification writes run in parallel with bet placement.
	notifMu     sync.Mutex   // notifications + audit log + login history
	blacklistMu sync.RWMutex // JWT blacklist
	refreshMu   sync.RWMutex // refresh tokens + refreshTokenTimes
	csrfMu      sync.RWMutex // CSRF tokens + csrfTokenTimes
	otpMu       sync.RWMutex // OTP store
	loginMu     sync.RWMutex // loginAttempts (brute force)

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

	// Per-user/per-market bet indexes for O(1) lookups. Populated alongside
	// every write to s.bets under s.mu so readers can avoid linear scans of
	// the entire bet map from HoldAndPlaceBet / SettleMarket / VoidMarket.
	betsByUser           map[int64]map[string]*Bet   // user_id -> bet_id -> bet
	betsByMarket         map[string]map[string]*Bet  // market_id -> bet_id -> bet
	clientRefs           map[int64]map[string]string // user_id -> client_ref -> bet_id
	exposureByUserMarket map[int64]map[string]float64 // user_id -> market_id -> open exposure

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

	// Async notification outbox: bet placement enqueues DB writes here so the
	// request hot path never blocks on 5–8 synchronous notification/audit
	// inserts. A small background worker pool drains the channel and performs
	// the actual dbAddNotification / dbAddAudit calls.
	notifChan chan notifJob
	notifWG   sync.WaitGroup

	// Cached superadmin user IDs so NotifyHierarchy does not have to scan the
	// entire user map under RLock on every bet. Refreshed when a superadmin
	// is created or has their status changed.
	superadminIDsMu sync.RWMutex
	superadminIDs   []int64
}

// notifJob is the unit of work for the background notification/audit worker.
// Kind is either "notification" or "audit". Only the fields relevant to that
// kind are populated.
type notifJob struct {
	Kind     string // "notification" or "audit"
	NotifID  string
	UserID   int64
	Username string
	Action   string
	Type     string
	Title    string
	Message  string
	Details  string
	IP       string
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
	DisplaySide    string  `json:"display_side"`
	MarketType     string  `json:"market_type"`
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
	// 1. Append to the in-memory slice synchronously so the current request's
	//    GET /api/v1/notifications view still observes the new entry.
	s.notifMu.Lock()
	s.notifications = append(s.notifications, n)
	s.notifMu.Unlock()

	// 2. Enqueue the DB write to the background worker pool with bounded
	//    backpressure. Previously this dropped silently when the channel
	//    was full (~4096 jobs), losing audit trail under settlement bursts.
	//    Now we wait up to 250ms for space; if the worker is still
	//    catching up after that we log at ERROR (not warn) and increment
	//    a drop counter so the next regression is loudly visible.
	if !useDB() {
		return
	}
	job := notifJob{
		Kind:    "notification",
		NotifID: nid,
		UserID:  userID,
		Type:    typ,
		Title:   title,
		Message: message,
	}
	select {
	case s.notifChan <- job:
		return
	default:
	}
	// Slow path: queue is full. Block briefly to apply backpressure
	// rather than silently dropping.
	t := time.NewTimer(250 * time.Millisecond)
	defer t.Stop()
	select {
	case s.notifChan <- job:
		return
	case <-t.C:
		notifDropCounter.Add(1)
		if logger != nil {
			logger.Error("notification queue overflow — dropping DB write after 250ms wait",
				"user_id", userID, "type", typ,
				"dropped_total", notifDropCounter.Load())
		}
	}
}

// notifDropCounter tracks how many notification DB writes we've dropped
// due to outbox backpressure timeout. Exported via the metrics endpoint
// so the next regression is alertable.
var notifDropCounter atomic.Int64

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
// Superadmin lookup uses the cached list so we avoid an O(N) scan of all users on every bet.
func (s *Store) collectHierarchyIDs(childID int64) []int64 {
	s.mu.RLock()
	child := s.users[childID]
	if child == nil {
		s.mu.RUnlock()
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
	s.mu.RUnlock()

	// Merge in cached superadmin IDs (dedup against parent chain).
	for _, saID := range s.getSuperadminIDs() {
		found := false
		for _, pid := range parentIDs {
			if pid == saID {
				found = true
				break
			}
		}
		if !found {
			parentIDs = append(parentIDs, saID)
		}
	}
	return parentIDs
}

func (s *Store) GetNotifications(userID int64, unreadOnly bool, limit, offset int) []*Notification {
	if useDB() {
		return dbGetNotifications(userID, unreadOnly, limit, offset)
	}
	s.notifMu.Lock()
	defer s.notifMu.Unlock()

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
	s.notifMu.Lock()
	defer s.notifMu.Unlock()
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
	s.notifMu.Lock()
	defer s.notifMu.Unlock()
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
	s.notifMu.Lock()
	defer s.notifMu.Unlock()
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
		betsByUser:           make(map[int64]map[string]*Bet),
		betsByMarket:         make(map[string]map[string]*Bet),
		clientRefs:           make(map[int64]map[string]string),
		exposureByUserMarket: make(map[int64]map[string]float64),
		// Buffered to absorb bursts of hierarchy fan-out (up to ~8 jobs per bet).
		// If full, writers drop the DB write rather than block the request handler.
		notifChan: make(chan notifJob, 4096),
		PrivateKey: priv,
		PublicKey:  pub,
	}
	s.nextUserID.Store(0)
	s.nextLedgerID.Store(0)

	// If DB is connected, load users from PostgreSQL into memory cache
	if useDB() {
		s.loadUsersFromDB()
	}

	s.seedData()

	// Prime the superadmin ID cache now that seedData (and any DB load) is done.
	s.refreshSuperadminCache()

	// Start background notification/audit workers. Two workers give us enough
	// parallelism to absorb bursts without serializing DB writes end-to-end.
	for i := 0; i < 2; i++ {
		s.notifWG.Add(1)
		go s.notificationWorker()
	}

	// Background cleanup: expired blacklist tokens, OTPs, old audit logs
	go s.startCleanupLoop()

	return s
}

// notificationWorker drains notifChan and performs the actual DB writes.
// Runs until notifChan is closed (by Stop), then exits.
//
// Each job is processed inside a dedicated function so a panic in one
// (corrupt row, malformed JSON, momentary DB connectivity hiccup that
// drops a connection mid-call) is recovered and logged instead of
// taking down the worker. Without this, a single bad job kills the
// worker, the queue fills, and every subsequent notification/audit
// write is silently dropped until the next process restart.
func (s *Store) notificationWorker() {
	defer s.notifWG.Done()
	for job := range s.notifChan {
		s.processNotifJobSafely(job)
	}
}

func (s *Store) processNotifJobSafely(job notifJob) {
	defer func() {
		if r := recover(); r != nil {
			notifWorkerPanicCounter.Add(1)
			if logger != nil {
				logger.Error("notification worker panic recovered",
					"kind", job.Kind,
					"user_id", job.UserID,
					"panic", fmt.Sprint(r),
					"panic_total", notifWorkerPanicCounter.Load())
			}
		}
	}()
	if !useDB() {
		return
	}
	switch job.Kind {
	case "notification":
		dbAddNotification(job.NotifID, job.UserID, job.Type, job.Title, job.Message)
	case "audit":
		dbAddAudit(job.UserID, job.Username, job.Action, job.Details, job.IP)
	}
}

// notifWorkerPanicCounter tracks recovered panics in the worker so the
// metrics endpoint can alert on a non-zero rate.
var notifWorkerPanicCounter atomic.Int64

// Stop closes the notification channel and waits for in-flight jobs to drain.
// Called from main during graceful shutdown so pending DB writes are not lost.
func (s *Store) Stop() {
	close(s.notifChan)
	s.notifWG.Wait()
}

// refreshSuperadminCache rebuilds the cached list of superadmin user IDs.
// Called after CreateUser for superadmin roles, status changes that might
// affect a superadmin, and at startup once seeding / DB load is finished.
func (s *Store) refreshSuperadminCache() {
	s.mu.RLock()
	var ids []int64
	for id, u := range s.users {
		if u.Role == "superadmin" {
			ids = append(ids, id)
		}
	}
	s.mu.RUnlock()

	s.superadminIDsMu.Lock()
	s.superadminIDs = ids
	s.superadminIDsMu.Unlock()
}

// getSuperadminIDs returns a snapshot of the cached superadmin IDs.
// Callers must not mutate the returned slice.
func (s *Store) getSuperadminIDs() []int64 {
	s.superadminIDsMu.RLock()
	defer s.superadminIDsMu.RUnlock()
	return s.superadminIDs
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

		// Each of these now uses its dedicated mutex so cleanup doesn't
		// block bet placement or user reads.
		s.blacklistMu.Lock()
		for token, exp := range s.blacklist {
			if now.After(exp) {
				delete(s.blacklist, token)
			}
		}
		s.blacklistMu.Unlock()

		s.otpMu.Lock()
		for uid, entry := range s.otpStore {
			if now.After(entry.Expiry) {
				delete(s.otpStore, uid)
			}
		}
		s.otpMu.Unlock()

		s.notifMu.Lock()
		if len(s.auditLog) > 10000 {
			s.auditLog = s.auditLog[len(s.auditLog)-10000:]
		}
		if len(s.loginHistory) > 5000 {
			s.loginHistory = s.loginHistory[len(s.loginHistory)-5000:]
		}
		s.notifMu.Unlock()

		s.csrfMu.Lock()
		for token, created := range s.csrfTokenTimes {
			if now.Sub(created) > 24*time.Hour {
				delete(s.csrfTokens, token)
				delete(s.csrfTokenTimes, token)
			}
		}
		s.csrfMu.Unlock()

		s.refreshMu.Lock()
		for token, created := range s.refreshTokenTimes {
			if now.Sub(created) > 7*24*time.Hour {
				delete(s.refreshTokens, token)
				delete(s.refreshTokenTimes, token)
			}
		}
		s.refreshMu.Unlock()

		s.loginMu.Lock()
		for username, attempt := range s.loginAttempts {
			if now.Sub(attempt.LastTry) > 30*time.Minute {
				delete(s.loginAttempts, username)
			}
		}
		s.loginMu.Unlock()
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

func (s *Store) CreateUser(username, email, password, role string, parentID *int64, creditLimit, commRate float64) (u *User, err error) {
	s.mu.Lock()
	needSuperadminRefresh := false
	defer func() {
		s.mu.Unlock()
		if needSuperadminRefresh {
			s.refreshSuperadminCache()
		}
	}()

	if _, exists := s.usersByName[username]; exists {
		return nil, fmt.Errorf("username already taken")
	}

	// Determine initial balance and validate parent BEFORE allocating IDs
	// or persisting anything, so a failed parent check leaves no orphaned
	// state in either store.
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
		initialBalance = creditLimit
	}

	now := time.Now().Format(time.RFC3339)
	passwordHash := hashPassword(password)

	// DB-first when persistence is on. The database owns the user ID via
	// SERIAL, so we use the returned ID as the source of truth and align
	// nextUserID to it. Without this the in-memory auto-increment and the
	// DB sequence drift apart on every restart, and bets recorded in DB
	// against user_id=N may point at a totally different user after a
	// reseed. Path is computed from the DB ID so the hierarchy string
	// also matches what loadUsersFromDB will see on the next boot.
	var id int64
	if useDB() {
		// Provisional path so the row is insertable; will be updated to
		// "<parent.path>.<id>" once we know the real ID.
		dbu, dberr := dbCreateUser(username, email, passwordHash, role, "", parentID, initialBalance, creditLimit, commRate, false)
		if dberr != nil {
			return nil, fmt.Errorf("create user (db): %w", dberr)
		}
		id = dbu.ID
		// Compute final path now that we have the DB-assigned ID.
		var finalPath string
		if parentID != nil {
			if p, ok := s.users[*parentID]; ok {
				finalPath = p.Path + "." + fmt.Sprintf("%d", id)
			} else {
				finalPath = fmt.Sprintf("%d", id)
			}
		} else {
			finalPath = fmt.Sprintf("%d", id)
		}
		// Persist the final path back to DB so loadUsersFromDB rebuilds
		// the same hierarchy string after restart.
		if _, perr := db.Exec(`UPDATE auth.users SET path=$1 WHERE id=$2`, finalPath, id); perr != nil {
			logger.Error("CreateUser: path update failed", "user_id", id, "error", perr)
		}
		// Keep the in-memory counter ahead of any DB-assigned ID so
		// in-memory-only fallback IDs (when useDB later goes false in
		// tests) never collide.
		for {
			cur := s.nextUserID.Load()
			if id <= cur || s.nextUserID.CompareAndSwap(cur, id) {
				break
			}
		}
		u = &User{
			ID: id, Username: username, Email: email,
			PasswordHash: passwordHash,
			Role: role, Path: finalPath, ParentID: parentID,
			Balance: initialBalance, Exposure: 0,
			CreditLimit: creditLimit, CommissionRate: commRate,
			Status: "active", ReferralCode: dbu.ReferralCode,
			CreatedAt: now, UpdatedAt: now,
		}
	} else {
		// In-memory-only fallback (no DATABASE_URL)
		id = s.nextUserID.Add(1)
		path := fmt.Sprintf("%d", id)
		if parentID != nil {
			if p, ok := s.users[*parentID]; ok {
				path = p.Path + "." + fmt.Sprintf("%d", id)
			}
		}
		refCode := fmt.Sprintf("REF-%s-%s", strings.ToUpper(username), randHex(2))
		u = &User{
			ID: id, Username: username, Email: email,
			PasswordHash: passwordHash,
			Role: role, Path: path, ParentID: parentID,
			Balance: initialBalance, Exposure: 0,
			CreditLimit: creditLimit, CommissionRate: commRate,
			Status: "active", ReferralCode: refCode,
			CreatedAt: now, UpdatedAt: now,
		}
	}

	// Apply parent debit AFTER successful child creation. Order matters:
	// if dbCreateUser failed above we returned early with no parent
	// mutation, leaving funds intact.
	if parentID != nil && creditLimit > 0 && role != "superadmin" {
		if parent, ok := s.users[*parentID]; ok {
			parent.Balance = roundMoney(parent.Balance - creditLimit)
			if useDB() {
				dbUpdateBalance(parent.ID, parent.Balance, parent.Exposure)
			}
		}
	}

	s.users[id] = u
	s.usersByName[username] = u
	if u.ReferralCode != "" {
		s.referralCodes[u.ReferralCode] = id
	}

	if role == "superadmin" {
		needSuperadminRefresh = true
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
		// Sync balance from DB for freshness — mutation and copy must happen
		// under the same write lock to avoid a data race with readers.
		if useDB() {
			bal, exp := dbSyncUserBalance(u.ID)
			s.mu.Lock()
			u.Balance = bal
			u.Exposure = exp
			cp := *u
			s.mu.Unlock()
			return &cp
		}
		// Return a copy so callers cannot race on the struct fields.
		s.mu.RLock()
		cp := *u
		s.mu.RUnlock()
		return &cp
	}
	// Fallback: check DB directly (user created by another instance)
	if useDB() {
		u = dbGetUserByUsername(username)
		if u != nil {
			s.mu.Lock()
			s.users[u.ID] = u
			s.usersByName[u.Username] = u
			cp := *u
			s.mu.Unlock()
			return &cp
		}
	}
	return u
}

// GetUserStatus returns the user's current status string ("active",
// "suspended", "blocked", etc.) without doing the DB sync that GetUser
// performs. Designed to be called from the auth middleware on every
// request, so the cost must stay at one map lookup under an RLock —
// same order as the blacklist check that already runs there.
func (s *Store) GetUserStatus(id int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.users[id]; ok {
		return u.Status
	}
	return ""
}

func (s *Store) GetUser(id int64) *User {
	s.mu.RLock()
	u := s.users[id]
	s.mu.RUnlock()
	if u != nil {
		// Sync balance from DB for freshness — mutation and copy must happen
		// under the same write lock to avoid a data race with readers.
		if useDB() {
			bal, exp := dbSyncUserBalance(u.ID)
			s.mu.Lock()
			u.Balance = bal
			u.Exposure = exp
			cp := *u
			s.mu.Unlock()
			return &cp
		}
		s.mu.RLock()
		cp := *u
		s.mu.RUnlock()
		return &cp
	}
	if useDB() {
		u = dbGetUser(id)
		if u != nil {
			s.mu.Lock()
			s.users[u.ID] = u
			s.usersByName[u.Username] = u
			cp := *u
			s.mu.Unlock()
			return &cp
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
	if useDB() {
		dbUpdateBalance(userID, u.Balance, u.Exposure)
	}
}

// ─── Bet index helpers ──────────────────────────────────────────────────────
//
// These helpers keep the per-user / per-market bet indexes consistent with
// s.bets. Callers MUST hold s.mu.Lock() before invoking them.

// betLiability returns the amount of exposure a bet contributes while it's
// still open (unmatched/matched/partial). Mirrors the calculation in the old
// O(N) scan inside HoldAndPlaceBet so the exposure index matches byte-for-byte.
func betLiability(b *Bet) float64 {
	if b.Side == "back" {
		return b.Stake
	}
	return b.Stake * (b.Price - 1)
}

// indexBetLocked inserts a newly-created bet into all secondary indexes.
// Must be called under s.mu.Lock().
func (s *Store) indexBetLocked(b *Bet) {
	if _, ok := s.betsByUser[b.UserID]; !ok {
		s.betsByUser[b.UserID] = make(map[string]*Bet)
	}
	s.betsByUser[b.UserID][b.ID] = b

	if _, ok := s.betsByMarket[b.MarketID]; !ok {
		s.betsByMarket[b.MarketID] = make(map[string]*Bet)
	}
	s.betsByMarket[b.MarketID][b.ID] = b

	if b.ClientRef != "" {
		if _, ok := s.clientRefs[b.UserID]; !ok {
			s.clientRefs[b.UserID] = make(map[string]string)
		}
		s.clientRefs[b.UserID][b.ClientRef] = b.ID
	}

	if b.Status == "unmatched" || b.Status == "matched" || b.Status == "partial" {
		if _, ok := s.exposureByUserMarket[b.UserID]; !ok {
			s.exposureByUserMarket[b.UserID] = make(map[string]float64)
		}
		s.exposureByUserMarket[b.UserID][b.MarketID] += betLiability(b)
	}
}

// releaseExposureLocked deducts a bet's open exposure from the per-user /
// per-market index. Called when a bet transitions out of an "open" state
// (settled, cancelled, void). Must hold s.mu.Lock().
func (s *Store) releaseExposureLocked(b *Bet) {
	if byMkt, ok := s.exposureByUserMarket[b.UserID]; ok {
		byMkt[b.MarketID] -= betLiability(b)
		if byMkt[b.MarketID] <= 0 {
			delete(byMkt, b.MarketID)
		}
		if len(byMkt) == 0 {
			delete(s.exposureByUserMarket, b.UserID)
		}
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

// HoldAndPlaceBet atomically validates balance, exposure limits, duplicate
// client_ref, holds funds, and places the bet — all under a single lock.
// This eliminates the TOCTOU race between validation and execution.
//
// Performance: the validation phase uses O(1) index lookups instead of
// scanning s.bets, and DB writes are lifted out of the critical section.
func (s *Store) HoldAndPlaceBet(userID int64, marketID string, selectionID int64, side string, price, stake float64, clientRef string, holdAmount float64) (*MatchResult, error) {
	// ── Phase 1: validate + reserve in memory under lock (no DB calls) ──
	s.mu.Lock()

	u, ok := s.users[userID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("user not found")
	}
	if u.Available() < holdAmount {
		avail := u.Available()
		s.mu.Unlock()
		return nil, fmt.Errorf("insufficient balance: available %.2f, required %.2f", avail, holdAmount)
	}

	m, marketExists := s.markets[marketID]
	if !marketExists {
		s.mu.Unlock()
		return nil, fmt.Errorf("market not found")
	}
	if m.Status != "open" {
		status := m.Status
		s.mu.Unlock()
		return nil, fmt.Errorf("market is %s, cannot place bets", status)
	}

	// O(1) per-market exposure check via the index (replaces scan of s.bets).
	existingExposure := 0.0
	if byMkt, hasIdx := s.exposureByUserMarket[userID]; hasIdx {
		existingExposure = byMkt[marketID]
	}
	maxMarketExposure := u.Balance * 0.5
	if existingExposure+holdAmount > maxMarketExposure && maxMarketExposure > 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("market exposure limit exceeded: existing %.0f + new %.0f > limit %.0f (50%% of balance)", existingExposure, holdAmount, maxMarketExposure)
	}

	// O(1) duplicate client_ref check via the index.
	if clientRef != "" {
		if refs, hasRefs := s.clientRefs[userID]; hasRefs {
			if _, dup := refs[clientRef]; dup {
				s.mu.Unlock()
				return nil, fmt.Errorf("duplicate bet: client_ref already used")
			}
		}
	}

	// Reserve exposure and build the bet record in memory.
	u.Exposure = roundMoney(u.Exposure + holdAmount)
	now := time.Now()
	nowStr := now.Format(time.RFC3339)
	betID := "bet-" + randHex(8)

	// Determine market type and display side (market is held under the lock).
	mType := m.MarketType
	var dSide string
	if mType == "fancy" || mType == "session" {
		if side == "back" {
			dSide = "yes"
		} else {
			dSide = "no"
		}
	} else {
		dSide = side
	}

	// House model: every bet is immediately fully matched.
	bet := &Bet{
		ID: betID, MarketID: marketID, SelectionID: selectionID,
		UserID: userID, Side: side, DisplaySide: dSide, MarketType: mType,
		Price: price, Stake: stake,
		MatchedStake: stake, UnmatchedStake: 0,
		Status: "matched", ClientRef: clientRef, CreatedAt: nowStr,
	}
	s.bets[betID] = bet
	s.indexBetLocked(bet)
	m.TotalMatched += stake

	// Append the in-memory ledger entry (no DB call yet — we'll batch below).
	ledgerID := s.nextLedgerID.Add(1)
	s.ledger = append(s.ledger, &LedgerEntry{
		ID: ledgerID, UserID: userID, Amount: -holdAmount,
		Type: "hold", Reference: "hold:" + betID, BetID: betID, CreatedAt: nowStr,
	})

	// Snapshot the fields we need for DB writes before releasing the lock.
	userBal := u.Balance
	userExp := u.Exposure
	betSnap := *bet
	s.mu.Unlock()

	// ── Phase 2: persist outside the critical section ──
	if useDB() {
		dbUpdateBalance(userID, userBal, userExp)
		dbAddLedger(userID, -holdAmount, "hold", "hold:"+betID, betID)
		dbSaveBet(&betSnap)
	}

	return &MatchResult{
		BetID: betID, MatchedStake: stake,
		UnmatchedStake: 0, Status: "matched", Fills: nil,
	}, nil
}

func (s *Store) PlaceAndMatch(userID int64, marketID string, selectionID int64, side string, price, stake float64, clientRef string) (*MatchResult, error) {
	// Two-phase: mutate in-memory under s.mu.Lock, then persist to DB
	// outside the lock. The previous version held s.mu.Lock() through the
	// dbSaveBet network round-trip, serializing every bet on Postgres
	// latency. HoldAndPlaceBet already follows this pattern; matching it
	// here so seed bets, sample order book bets, and the alternative
	// placement path all get the same throughput.
	s.mu.Lock()

	betID := "bet-" + randHex(8)
	now := time.Now()

	// House model: every bet is immediately fully matched.
	status := "matched"

	// Determine market type and display side
	var mType, dSide string
	if m, ok := s.markets[marketID]; ok {
		mType = m.MarketType
	}
	if mType == "fancy" || mType == "session" {
		if side == "back" {
			dSide = "yes"
		} else {
			dSide = "no"
		}
	} else {
		dSide = side
	}

	bet := &Bet{
		ID: betID, MarketID: marketID, SelectionID: selectionID,
		UserID: userID, Side: side, DisplaySide: dSide, MarketType: mType,
		Price: price, Stake: stake,
		MatchedStake: stake, UnmatchedStake: 0,
		Status: status, ClientRef: clientRef, CreatedAt: now.Format(time.RFC3339),
	}
	s.bets[betID] = bet
	s.indexBetLocked(bet)

	if m, ok := s.markets[marketID]; ok {
		m.TotalMatched += stake
	}

	// Snapshot for use after lock release
	betSnap := *bet
	s.mu.Unlock()

	if useDB() {
		dbSaveBet(&betSnap)
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
				if useDB() {
					dbUpdateBalance(bet.UserID, u.Balance, u.Exposure)
				}
			}
			s.orderBooks[key] = append(orders[:i], orders[i+1:]...)
			// If the bet is being fully cancelled, remove its exposure
			// contribution from the index. Partial cancels keep the
			// matched portion live so we leave the index entry alone.
			if bet.MatchedStake == 0 {
				s.releaseExposureLocked(bet)
				bet.Status = "cancelled"
			} else {
				bet.Status = "partial"
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

		// Release this bet's contribution from the per-user exposure index.
		s.releaseExposureLocked(bet)
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

		// Release exposure. CRITICAL: must use the same liability formula
		// as betLiability() — for lay bets that's stake*(price-1), NOT just
		// matched_stake. Releasing only matched_stake leaks the difference
		// permanently and shrinks the user's available balance every time.
		exposureToRelease := bet.MatchedStake
		if bet.Side == "lay" {
			exposureToRelease = roundMoney(bet.MatchedStake * (bet.Price - 1))
		}
		u.Exposure = roundMoney(math.Max(u.Exposure-exposureToRelease, 0))
		// Apply P&L to user (after commission deduction)
		u.Balance = roundMoney(u.Balance + pnl - commission)

		now := time.Now().Format(time.RFC3339)
		// Ledger MUST mirror the exposure release amount, not the raw stake.
		// For lay bets that's stake*(price-1); using MatchedStake here would
		// leave the audit trail unreconcilable against balance+exposure.
		s.addLedger(bet.UserID, exposureToRelease, "release", "release:"+bet.ID, bet.ID, now)
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
		s.releaseExposureLocked(bet)
		bet.Status = "void"
		voided++

		if u, ok := s.users[bet.UserID]; ok {
			// Release the FULL liability (which is what was held) — not
			// just the stake. For lays this is stake*(price-1).
			exposureToRelease := betLiability(bet)
			u.Exposure = math.Max(u.Exposure-exposureToRelease, 0)
			now := time.Now().Format(time.RFC3339)
			s.addLedger(bet.UserID, exposureToRelease, "release", "void:"+bet.ID, bet.ID, now)
			if useDB() {
				dbUpdateBalance(bet.UserID, u.Balance, u.Exposure)
			}
		}
		if useDB() {
			dbUpdateBet(bet)
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
	s.mu.Unlock()

	// Persist outside the lock so the DB call doesn't block other writers.
	// Without this the session vanishes on restart and the in-memory cache
	// drifts from the betting.casino_sessions table that other services
	// query.
	if useDB() {
		dbSaveCasinoSession(sess)
	}
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
	if useDB() {
		dbSavePaymentTx(tx)
	}
	return tx
}

func (s *Store) GetUserPayments(userID int64) []*PaymentTx {
	if useDB() {
		return dbGetUserPayments(userID)
	}
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
		if useDB() {
			return dbGetChildren(userID)
		}
		return nil
	}
	var out []*User
	for _, child := range s.users {
		if child.ID != userID && len(child.Path) > len(u.Path) && child.Path[:len(u.Path)] == u.Path {
			out = append(out, child)
		}
	}
	if len(out) == 0 && useDB() {
		return dbGetChildren(userID)
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
	if len(out) == 0 && useDB() {
		return dbGetDirectChildren(userID)
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
		if useDB() {
			return dbGetChildren(userID)
		}
		return nil
	}
	prefix := u.Path + "."
	var out []*User
	for _, child := range s.users {
		if child.ID != userID && strings.HasPrefix(child.Path, prefix) {
			out = append(out, child)
		}
	}
	if len(out) == 0 && useDB() {
		return dbGetChildren(userID)
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
	if useDB() {
		dbUpdateReferredBy(userID, referrerID)
	}
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
	s.otpMu.Lock()
	s.otpStore[userID] = &OTPEntry{Code: code, Expiry: time.Now().Add(5 * time.Minute)}
	s.otpMu.Unlock()
	return code
}

func (s *Store) VerifyOTP(userID int64, code string) bool {
	// Use a single write lock for atomic read+delete to prevent two concurrent
	// requests from verifying the same OTP (TOCTOU).
	s.otpMu.Lock()
	entry, ok := s.otpStore[userID]
	if !ok || time.Now().After(entry.Expiry) || entry.Code != code {
		s.otpMu.Unlock()
		return false
	}
	delete(s.otpStore, userID)
	s.otpMu.Unlock()
	return true
}

// ─── CSRF Tokens ────────────────────────────────────────────────────────────

func (s *Store) GenerateCSRF(userID int64) string {
	token := randHex(16)
	s.csrfMu.Lock()
	s.csrfTokens[token] = userID
	s.csrfTokenTimes[token] = time.Now()
	s.csrfMu.Unlock()
	return token
}

func (s *Store) ValidateCSRF(token string) bool {
	s.csrfMu.RLock()
	_, ok := s.csrfTokens[token]
	s.csrfMu.RUnlock()
	return ok
}

// ─── Brute Force Protection ─────────────────────────────────────────────────

func (s *Store) CheckLoginAttempt(username string) (bool, time.Time) {
	s.loginMu.RLock()
	attempt, ok := s.loginAttempts[username]
	s.loginMu.RUnlock()
	if !ok {
		return true, time.Time{} // allowed
	}
	if !attempt.LockedUntil.IsZero() && time.Now().Before(attempt.LockedUntil) {
		return false, attempt.LockedUntil
	}
	return true, time.Time{}
}

func (s *Store) RecordFailedLogin(username string) (bool, time.Time) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

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
	s.loginMu.Lock()
	delete(s.loginAttempts, username)
	s.loginMu.Unlock()
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
	s.notifMu.Lock()
	s.auditLog = append(s.auditLog, entry)
	s.notifMu.Unlock()

	// Off-load the DB insert to the background worker so the request hot path
	// is not blocked. Drop with a warning if the channel is full rather than
	// back-pressure the request handler.
	if !useDB() {
		return
	}
	select {
	case s.notifChan <- notifJob{
		Kind:     "audit",
		UserID:   userID,
		Username: username,
		Action:   action,
		Details:  details,
		IP:       ip,
	}:
	default:
		if logger != nil {
			logger.Warn("audit queue full, dropping DB write", "user_id", userID, "action", action)
		}
	}
}

func (s *Store) GetAuditLog(userID int64, role string) []*AuditEntry {
	if useDB() {
		if role == "superadmin" {
			return dbGetAuditLog(500)
		}
		return dbGetAuditLogForUser(userID, 500)
	}

	if role == "superadmin" {
		s.notifMu.Lock()
		out := make([]*AuditEntry, len(s.auditLog))
		copy(out, s.auditLog)
		s.notifMu.Unlock()
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		if len(out) > 500 {
			out = out[:500]
		}
		return out
	}

	// Snapshot downline IDs under s.mu (users map), then scan auditLog
	// under notifMu. Avoids holding two locks simultaneously.
	s.mu.RLock()
	u := s.users[userID]
	if u == nil {
		s.mu.RUnlock()
		return nil
	}
	prefix := u.Path + "."
	downlineIDs := map[int64]bool{userID: true}
	for _, child := range s.users {
		if child.ID != userID && strings.HasPrefix(child.Path, prefix) {
			downlineIDs[child.ID] = true
		}
	}
	s.mu.RUnlock()

	s.notifMu.Lock()
	defer s.notifMu.Unlock()
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
	s.notifMu.Lock()
	s.loginHistory = append(s.loginHistory, record)
	s.notifMu.Unlock()
	if useDB() {
		dbRecordLogin(userID, ip, userAgent, success)
	}
}

func (s *Store) GetLoginHistory(userID int64, limit int) []*LoginRecord {
	if useDB() {
		return dbGetLoginHistory(userID, limit)
	}
	s.notifMu.Lock()
	defer s.notifMu.Unlock()

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
