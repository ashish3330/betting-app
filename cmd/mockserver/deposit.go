package main

// ══════════════════════════════════════════════════════════════════
// Deposit Payment Module
// ══════════════════════════════════════════════════════════════════
//
// Bank accounts belong to Masters or Agents.
// Players request deposits → system picks an account with remaining
// daily limit (< 90K INR) → returns QR + details → Agent/Master
// confirms → wallet credited.
//
// Daily limit is tracked per bank account per calendar day in IST.
// All money stored as float64 (in production use decimal).
// Uses Store.mu for concurrency safety (in production use DB locks).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Types ───────────────────────────────────────────────────────

type BankAccount struct {
	ID             int64   `json:"id"`
	OwnerID        int64   `json:"owner_id"`
	OwnerRole      string  `json:"owner_role"` // "master" or "agent"
	BankName       string  `json:"bank_name"`
	AccountHolder  string  `json:"account_holder"`
	AccountNumber  string  `json:"account_number"`
	IFSCCode       string  `json:"ifsc_code"`
	UPIID          string  `json:"upi_id,omitempty"`
	QRImageURL     string  `json:"qr_image_url,omitempty"`
	DailyLimit     float64 `json:"daily_limit"`
	Status         string  `json:"status"` // "active" or "inactive"
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`

	// Computed fields (not stored)
	UsedToday      float64 `json:"used_today"`
	RemainingLimit float64 `json:"remaining_limit"`
	DepositCount   int     `json:"deposit_count_today"`
	OwnerUsername  string  `json:"owner_username,omitempty"`
}

type DailyUsage struct {
	BankAccountID int64   `json:"bank_account_id"`
	UsageDate     string  `json:"usage_date"`
	TotalUsed     float64 `json:"total_used"`
	DepositCount  int     `json:"deposit_count"`
}

type DepositRequest struct {
	ID              int64   `json:"id"`
	PlayerID        int64   `json:"player_id"`
	PlayerUsername  string  `json:"player_username,omitempty"`
	AgentID         int64   `json:"agent_id"`
	MasterID        int64   `json:"master_id"`
	BankAccountID   int64   `json:"bank_account_id"`
	Amount          float64 `json:"amount"`
	Status          string  `json:"status"` // pending, confirmed, rejected, expired
	ConfirmedBy     int64   `json:"confirmed_by,omitempty"`
	ConfirmedAt     string  `json:"confirmed_at,omitempty"`
	TxnReference    string  `json:"txn_reference,omitempty"`
	RejectionReason string  `json:"rejection_reason,omitempty"`
	CreatedAt       string  `json:"created_at"`

	// Denormalized for display
	BankName       string  `json:"bank_name,omitempty"`
	AccountLast4   string  `json:"account_last4,omitempty"`
}

// ── Deposit Store ───────────────────────────────────────────────

type DepositStore struct {
	mu              sync.RWMutex
	bankAccounts    map[int64]*BankAccount
	nextBankAcctID  atomic.Int64
	dailyUsage      map[string]*DailyUsage // key: "accountID:YYYY-MM-DD"
	depositRequests map[int64]*DepositRequest
	nextDepositID   atomic.Int64
}

func NewDepositStore() *DepositStore {
	ds := &DepositStore{
		bankAccounts:    make(map[int64]*BankAccount),
		dailyUsage:      make(map[string]*DailyUsage),
		depositRequests: make(map[int64]*DepositRequest),
	}
	ds.nextBankAcctID.Store(0)
	ds.nextDepositID.Store(0)
	return ds
}

const dailyLimitINR = 90000.00

// todayIST returns today's date string in IST
func todayIST() string {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return time.Now().In(loc).Format("2006-01-02")
}

// usageKey creates the map key for daily usage
func usageKey(accountID int64, date string) string {
	return fmt.Sprintf("%d:%s", accountID, date)
}

// ── Bank Account CRUD ───────────────────────────────────────────

func (ds *DepositStore) CreateBankAccount(ownerID int64, ownerRole string, req *BankAccount) *BankAccount {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	id := ds.nextBankAcctID.Add(1)
	now := time.Now().Format(time.RFC3339)

	acct := &BankAccount{
		ID:            id,
		OwnerID:       ownerID,
		OwnerRole:     ownerRole,
		BankName:      req.BankName,
		AccountHolder: req.AccountHolder,
		AccountNumber: req.AccountNumber,
		IFSCCode:      req.IFSCCode,
		UPIID:         req.UPIID,
		QRImageURL:    req.QRImageURL,
		DailyLimit:    dailyLimitINR,
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	ds.bankAccounts[id] = acct

	if useDB() {
		dbID := dbSaveBankAccount(acct)
		if dbID > 0 {
			acct.ID = dbID
			delete(ds.bankAccounts, id)
			ds.bankAccounts[dbID] = acct
		}
	}

	return acct
}

func (ds *DepositStore) UpdateBankAccount(id int64, ownerID int64, updates map[string]string) (*BankAccount, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	acct, ok := ds.bankAccounts[id]
	if !ok {
		return nil, fmt.Errorf("bank account not found")
	}
	if acct.OwnerID != ownerID {
		return nil, fmt.Errorf("not authorized to update this account")
	}

	if v, ok := updates["bank_name"]; ok { acct.BankName = v }
	if v, ok := updates["account_holder"]; ok { acct.AccountHolder = v }
	if v, ok := updates["account_number"]; ok { acct.AccountNumber = v }
	if v, ok := updates["ifsc_code"]; ok { acct.IFSCCode = v }
	if v, ok := updates["upi_id"]; ok { acct.UPIID = v }
	if v, ok := updates["qr_image_url"]; ok { acct.QRImageURL = v }
	if v, ok := updates["status"]; ok && (v == "active" || v == "inactive") { acct.Status = v }
	acct.UpdatedAt = time.Now().Format(time.RFC3339)

	if useDB() {
		dbUpdateBankAccount(acct)
	}

	return acct, nil
}

func (ds *DepositStore) DeleteBankAccount(id int64, ownerID int64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	acct, ok := ds.bankAccounts[id]
	if !ok {
		return fmt.Errorf("bank account not found")
	}
	if acct.OwnerID != ownerID {
		return fmt.Errorf("not authorized")
	}
	acct.Status = "inactive"

	if useDB() {
		dbUpdateBankAccount(acct)
	}

	return nil
}

// GetUsageToday returns today's usage for a bank account
func (ds *DepositStore) GetUsageToday(accountID int64) (float64, int) {
	key := usageKey(accountID, todayIST())
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if u, ok := ds.dailyUsage[key]; ok {
		return u.TotalUsed, u.DepositCount
	}
	if useDB() {
		return dbGetDailyUsage(accountID, todayIST())
	}
	return 0, 0
}

// enrichAccount adds computed fields (used_today, remaining_limit)
func (ds *DepositStore) enrichAccount(acct *BankAccount) {
	used, count := ds.GetUsageToday(acct.ID)
	acct.UsedToday = used
	acct.RemainingLimit = math.Max(acct.DailyLimit-used, 0)
	acct.DepositCount = count
}

// GetBankAccountsByOwner returns accounts owned by a specific user
func (ds *DepositStore) GetBankAccountsByOwner(ownerID int64) []*BankAccount {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var out []*BankAccount
	for _, acct := range ds.bankAccounts {
		if acct.OwnerID == ownerID {
			clone := *acct
			ds.enrichAccount(&clone)
			out = append(out, &clone)
		}
	}

	// DB fallback when in-memory is empty
	if len(out) == 0 && useDB() {
		dbAccounts := dbGetBankAccountsByOwner(ownerID)
		for _, acct := range dbAccounts {
			ds.enrichAccount(acct)
		}
		return dbAccounts
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetBankAccountsForDownline returns accounts owned by user + ALL users in their downline tree
// Works for Admin (sees masters+agents), Master (sees agents), SuperAdmin (sees all)
func (ds *DepositStore) GetBankAccountsForMaster(userID int64, mainStore *Store) []*BankAccount {
	// Get ALL downline users (masters, agents, etc.)
	downlineIDs := map[int64]bool{userID: true}
	children := mainStore.GetDownlineUsers(userID)
	for _, u := range children {
		downlineIDs[u.ID] = true
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var out []*BankAccount
	for _, acct := range ds.bankAccounts {
		if downlineIDs[acct.OwnerID] {
			clone := *acct
			ds.enrichAccount(&clone)
			if owner := mainStore.GetUser(clone.OwnerID); owner != nil {
				clone.OwnerUsername = owner.Username
			}
			out = append(out, &clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetAllBankAccounts for SuperAdmin — includes owner username
func (ds *DepositStore) GetAllBankAccounts() []*BankAccount {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var out []*BankAccount
	for _, acct := range ds.bankAccounts {
		clone := *acct
		ds.enrichAccount(&clone)
		// Add owner username for SuperAdmin visibility
		if owner := store.GetUser(clone.OwnerID); owner != nil {
			clone.OwnerUsername = owner.Username
		}
		out = append(out, &clone)
	}

	// DB fallback when in-memory is empty
	if len(out) == 0 && useDB() {
		dbAccounts := dbGetAllBankAccounts()
		for _, acct := range dbAccounts {
			ds.enrichAccount(acct)
			if owner := store.GetUser(acct.OwnerID); owner != nil {
				acct.OwnerUsername = owner.Username
			}
		}
		return dbAccounts
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ── Account Selection (picks best account with remaining limit) ──

// SelectAvailableAccount finds an active bank account from the player's
// Agent or Master that still has remaining daily limit.
// Strategy: least-used-first (the account with the most remaining limit).
func (ds *DepositStore) SelectAvailableAccount(playerID int64, amount float64, mainStore *Store) (*BankAccount, error) {
	// Find player's agent and master
	player := mainStore.GetUser(playerID)
	if player == nil {
		return nil, fmt.Errorf("player not found")
	}
	if player.Role != "client" {
		return nil, fmt.Errorf("only players can request deposits")
	}
	if player.ParentID == nil {
		return nil, fmt.Errorf("player has no agent assigned")
	}

	agentID := *player.ParentID
	agent := mainStore.GetUser(agentID)
	if agent == nil {
		return nil, fmt.Errorf("agent not found")
	}

	// Find master (agent's parent)
	var masterID int64
	if agent.Role == "agent" && agent.ParentID != nil {
		masterID = *agent.ParentID
	} else if agent.Role == "master" {
		masterID = agent.ID
		agentID = 0 // player directly under master
	}

	// Collect candidate owner IDs (agent + master)
	candidateOwners := map[int64]bool{}
	if agentID > 0 {
		candidateOwners[agentID] = true
	}
	if masterID > 0 {
		candidateOwners[masterID] = true
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	today := todayIST()
	var bestAccount *BankAccount
	var bestRemaining float64 = -1

	for _, acct := range ds.bankAccounts {
		if acct.Status != "active" {
			continue
		}
		if !candidateOwners[acct.OwnerID] {
			continue
		}

		// Check daily usage
		key := usageKey(acct.ID, today)
		used := 0.0
		if u, ok := ds.dailyUsage[key]; ok {
			used = u.TotalUsed
		}
		remaining := acct.DailyLimit - used

		// Skip if not enough remaining for this deposit
		if remaining < amount {
			continue
		}

		// Pick account with most remaining limit (least used first)
		if remaining > bestRemaining {
			bestRemaining = remaining
			bestAccount = acct
		}
	}

	if bestAccount == nil {
		return nil, fmt.Errorf("no bank accounts available with sufficient daily limit. All accounts have reached the ₹%.0f daily limit or insufficient remaining balance for ₹%.0f", dailyLimitINR, amount)
	}

	// Return enriched copy
	clone := *bestAccount
	clone.UsedToday = bestAccount.DailyLimit - bestRemaining
	clone.RemainingLimit = bestRemaining
	return &clone, nil
}

// ── Deposit Request & Confirmation ──────────────────────────────

func (ds *DepositStore) CreateDepositRequest(playerID, agentID, masterID, bankAccountID int64, amount float64) *DepositRequest {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	id := ds.nextDepositID.Add(1)
	req := &DepositRequest{
		ID:            id,
		PlayerID:      playerID,
		AgentID:       agentID,
		MasterID:      masterID,
		BankAccountID: bankAccountID,
		Amount:        amount,
		Status:        "pending",
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	// Add bank account info for display
	if acct, ok := ds.bankAccounts[bankAccountID]; ok {
		req.BankName = acct.BankName
		if len(acct.AccountNumber) >= 4 {
			req.AccountLast4 = acct.AccountNumber[len(acct.AccountNumber)-4:]
		}
	}

	ds.depositRequests[id] = req

	if useDB() {
		dbID := dbSaveDepositRequest(req)
		if dbID > 0 {
			delete(ds.depositRequests, id)
			req.ID = dbID
			ds.depositRequests[dbID] = req
		}
	}

	return req
}

func (ds *DepositStore) ConfirmDeposit(depositID int64, confirmedBy int64, txnRef string, mainStore *Store) (*DepositRequest, error) {
	// Lock both stores in consistent order: mainStore first, then ds (prevents deadlock)
	mainStore.mu.Lock()
	ds.mu.Lock()

	req, ok := ds.depositRequests[depositID]
	if !ok {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("deposit request not found")
	}
	if req.Status != "pending" {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("deposit already %s", req.Status)
	}

	// Verify confirmer is the Agent or Master
	confirmer := mainStore.users[confirmedBy]
	if confirmer == nil {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("confirmer not found")
	}
	if confirmer.ID != req.AgentID && confirmer.ID != req.MasterID && confirmer.Role != "superadmin" {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("not authorized to confirm this deposit")
	}

	// Check daily limit before confirming (atomic: under same lock)
	today := todayIST()
	key := usageKey(req.BankAccountID, today)
	usage, usageExists := ds.dailyUsage[key]
	if !usageExists {
		usage = &DailyUsage{
			BankAccountID: req.BankAccountID,
			UsageDate:     today,
		}
		ds.dailyUsage[key] = usage
	}

	acct := ds.bankAccounts[req.BankAccountID]
	if acct == nil {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("bank account not found")
	}

	if usage.TotalUsed+req.Amount > acct.DailyLimit {
		ds.mu.Unlock()
		mainStore.mu.Unlock()
		return nil, fmt.Errorf("confirming this deposit would exceed the daily limit of ₹%.0f (current: ₹%.0f, deposit: ₹%.0f)", acct.DailyLimit, usage.TotalUsed, req.Amount)
	}

	// All checks passed — now atomically update everything
	usage.TotalUsed += req.Amount
	usage.DepositCount++

	req.Status = "confirmed"
	req.ConfirmedBy = confirmedBy
	req.ConfirmedAt = time.Now().Format(time.RFC3339)
	req.TxnReference = txnRef

	// Credit player wallet (already under mainStore.mu.Lock)
	if player, ok := mainStore.users[req.PlayerID]; ok {
		player.Balance += req.Amount
	}
	now := time.Now().Format(time.RFC3339)
	mainStore.addLedger(req.PlayerID, req.Amount, "deposit", fmt.Sprintf("deposit:%d", depositID), "", now)

	// Persist to DB
	if useDB() {
		dbUpdateDepositRequest(req)
		dbSaveDailyUsage(usage)
	}

	// Release both locks
	ds.mu.Unlock()
	mainStore.mu.Unlock()

	// Audit (uses its own lock)
	mainStore.AddAudit(confirmedBy, confirmer.Username, "deposit_confirmed",
		fmt.Sprintf("deposit=%d player=%d amount=%.2f txn=%s", depositID, req.PlayerID, req.Amount, txnRef), "")

	return req, nil
}

func (ds *DepositStore) RejectDeposit(depositID int64, rejectedBy int64, reason string) (*DepositRequest, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	req, ok := ds.depositRequests[depositID]
	if !ok {
		return nil, fmt.Errorf("deposit request not found")
	}
	if req.Status != "pending" {
		return nil, fmt.Errorf("deposit already %s", req.Status)
	}

	req.Status = "rejected"
	req.RejectionReason = reason
	req.ConfirmedBy = rejectedBy
	req.ConfirmedAt = time.Now().Format(time.RFC3339)

	if useDB() {
		dbUpdateDepositRequest(req)
	}

	return req, nil
}

// GetDepositRequests returns deposit requests filtered by role visibility
func (ds *DepositStore) GetDepositRequests(userID int64, role string, statusFilter string) []*DepositRequest {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var out []*DepositRequest
	for _, req := range ds.depositRequests {
		// Visibility check
		visible := false
		switch role {
		case "superadmin":
			visible = true
		case "admin", "master":
			visible = req.MasterID == userID || req.AgentID == userID
		case "agent":
			visible = req.AgentID == userID
		case "client":
			visible = req.PlayerID == userID
		}
		if !visible {
			continue
		}
		if statusFilter != "" && req.Status != statusFilter {
			continue
		}
		out = append(out, req)
	}

	// DB fallback when in-memory is empty
	if len(out) == 0 && useDB() {
		return dbGetDepositRequests(userID, role, statusFilter)
	}

	// Sort newest first
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// ── HTTP Handlers ───────────────────────────────────────────────

var depositStore *DepositStore

func registerDepositRoutes(mux *http.ServeMux) {
	depositStore = NewDepositStore()

	// Player endpoints
	mux.HandleFunc("GET /api/v1/deposit/available-accounts", auth(handleAvailableAccounts))
	mux.HandleFunc("POST /api/v1/deposit/request", auth(handleDepositRequest))
	mux.HandleFunc("POST /api/v1/deposit/extract-utr", auth(handleExtractUTR))
	mux.HandleFunc("GET /api/v1/deposit/requests", auth(handleGetDepositRequests))

	// Agent/Master endpoints
	mux.HandleFunc("POST /api/v1/deposit/confirm", auth(handleConfirmDeposit))
	mux.HandleFunc("POST /api/v1/deposit/reject", auth(handleRejectDeposit))

	// Bank account CRUD
	mux.HandleFunc("GET /api/v1/deposit/accounts", auth(handleGetBankAccounts))
	mux.HandleFunc("POST /api/v1/deposit/accounts", auth(handleCreateBankAccount))
	mux.HandleFunc("PUT /api/v1/deposit/accounts/{id}", auth(handleUpdateBankAccount))
	mux.HandleFunc("DELETE /api/v1/deposit/accounts/{id}", auth(handleDeleteBankAccount))

	// Usage dashboard + tree view
	mux.HandleFunc("GET /api/v1/deposit/usage", auth(handleGetUsageDashboard))
	mux.HandleFunc("GET /api/v1/deposit/tree", auth(handleDepositTree))

	// Seed bank accounts for testing
	seedBankAccounts()
}

func seedBankAccounts() {
	// Agent1 (id=4) bank accounts
	depositStore.mu.Lock()
	defer depositStore.mu.Unlock()

	accounts := []*BankAccount{
		{
			ID: 1, OwnerID: 4, OwnerRole: "agent",
			BankName: "State Bank of India", AccountHolder: "3XBet Payments",
			AccountNumber: "38291047562810", IFSCCode: "SBIN0001234",
			UPIID: "3xbet.pay@sbi", QRImageURL: "https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=upi://pay?pa=3xbet.pay@sbi",
			DailyLimit: dailyLimitINR, Status: "active",
		},
		{
			ID: 2, OwnerID: 4, OwnerRole: "agent",
			BankName: "HDFC Bank", AccountHolder: "3XBet Exchange",
			AccountNumber: "50100298374651", IFSCCode: "HDFC0002567",
			UPIID: "3xbet.hdfc@hdfcbank", QRImageURL: "https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=upi://pay?pa=3xbet.hdfc@hdfcbank",
			DailyLimit: dailyLimitINR, Status: "active",
		},
		{
			ID: 3, OwnerID: 3, OwnerRole: "master",
			BankName: "ICICI Bank", AccountHolder: "3XBet Master",
			AccountNumber: "19284756301928", IFSCCode: "ICIC0003456",
			UPIID: "3xbet.master@icici", QRImageURL: "https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=upi://pay?pa=3xbet.master@icici",
			DailyLimit: dailyLimitINR, Status: "active",
		},
		{
			ID: 4, OwnerID: 3, OwnerRole: "master",
			BankName: "Kotak Mahindra Bank", AccountHolder: "3XBet Services",
			AccountNumber: "74628193045726", IFSCCode: "KKBK0004567",
			UPIID: "3xbet.kotak@kotak", QRImageURL: "https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=upi://pay?pa=3xbet.kotak@kotak",
			DailyLimit: dailyLimitINR, Status: "active",
		},
	}

	for _, a := range accounts {
		if _, exists := depositStore.bankAccounts[a.ID]; !exists {
			depositStore.bankAccounts[a.ID] = a
			depositStore.nextBankAcctID.Store(int64(a.ID))
		}
	}
}

// GET /api/v1/deposit/available-accounts — Player sees accounts they can deposit to
func handleAvailableAccounts(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	player := store.GetUser(uid)
	if player == nil {
		writeErr(w, 404, "user not found")
		return
	}

	// Find agent and master in hierarchy
	var ownerIDs []int64
	if player.ParentID != nil {
		ownerIDs = append(ownerIDs, *player.ParentID)
		agent := store.GetUser(*player.ParentID)
		if agent != nil && agent.ParentID != nil {
			ownerIDs = append(ownerIDs, *agent.ParentID)
			master := store.GetUser(*agent.ParentID)
			if master != nil && master.ParentID != nil {
				ownerIDs = append(ownerIDs, *master.ParentID)
			}
		}
	}

	today := todayIST()
	depositStore.mu.RLock()
	defer depositStore.mu.RUnlock()

	// Parse requested amount from query param
	requestedAmount, _ := strconv.ParseFloat(r.URL.Query().Get("amount"), 64)
	if requestedAmount < 100 {
		requestedAmount = 100
	}

	type accountInfo struct {
		ID            int64  `json:"id"`
		BankName      string `json:"bank_name"`
		AccountHolder string `json:"account_holder"`
		AccountNumber string `json:"account_number"`
		IFSCCode      string `json:"ifsc_code"`
		UPIID         string `json:"upi_id"`
		QRImageURL    string `json:"qr_image_url"`
	}

	type rankedAccount struct {
		info      accountInfo
		remaining float64
	}

	var candidates []rankedAccount
	for _, acct := range depositStore.bankAccounts {
		if acct.Status != "active" {
			continue
		}
		isOwner := false
		for _, oid := range ownerIDs {
			if acct.OwnerID == oid {
				isOwner = true
				break
			}
		}
		if !isOwner {
			continue
		}

		used := 0.0
		key := usageKey(acct.ID, today)
		if usage, ok := depositStore.dailyUsage[key]; ok {
			used = usage.TotalUsed
		}
		remaining := acct.DailyLimit - used

		// Only show accounts that can accept the requested amount
		if remaining < requestedAmount {
			continue
		}

		candidates = append(candidates, rankedAccount{
			info: accountInfo{
				ID: acct.ID, BankName: acct.BankName, AccountHolder: acct.AccountHolder,
				AccountNumber: acct.AccountNumber, IFSCCode: acct.IFSCCode,
				UPIID: acct.UPIID, QRImageURL: acct.QRImageURL,
			},
			remaining: remaining,
		})
	}

	// Sort by remaining limit descending (best candidates first), return top 2
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].remaining > candidates[j].remaining
	})
	if len(candidates) > 2 {
		candidates = candidates[:2]
	}

	result := make([]accountInfo, len(candidates))
	for i, c := range candidates {
		result[i] = c.info
	}

	writeJSON(w, 200, result)
}

// POST /api/v1/deposit/extract-utr — Extract UTR from screenshot text (no image stored)
func handleExtractUTR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"` // OCR text from frontend
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}

	// Extract 12-digit UTR number from text
	// UTR formats: 12-digit numeric, or alphanumeric like XXXXXXNNNNNNNNNNNN
	utr := ""
	// Try pure 12-digit number
	for i := 0; i <= len(req.Text)-12; i++ {
		allDigit := true
		for j := 0; j < 12; j++ {
			if req.Text[i+j] < '0' || req.Text[i+j] > '9' {
				allDigit = false
				break
			}
		}
		if allDigit {
			candidate := req.Text[i : i+12]
			// UTR usually doesn't start with 0000
			if candidate[:4] != "0000" {
				utr = candidate
				break
			}
		}
	}

	// Try 16-22 char alphanumeric (bank ref numbers)
	if utr == "" {
		words := strings.Fields(req.Text)
		for _, w := range words {
			clean := strings.TrimSpace(w)
			if len(clean) >= 12 && len(clean) <= 22 {
				isAlphaNum := true
				hasDigit := false
				for _, c := range clean {
					if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
						isAlphaNum = false
						break
					}
					if c >= '0' && c <= '9' {
						hasDigit = true
					}
				}
				if isAlphaNum && hasDigit {
					utr = clean
					break
				}
			}
		}
	}

	// Extract amount from OCR text (look for ₹ or Rs or INR followed by number)
	var extractedAmount float64
	amountPatterns := []string{"₹", "Rs.", "Rs", "INR", "Amount", "Paid", "Debited"}
	lines := strings.Split(req.Text, "\n")
	for _, line := range lines {
		for _, pattern := range amountPatterns {
			idx := strings.Index(strings.ToLower(line), strings.ToLower(pattern))
			if idx < 0 {
				continue
			}
			// Scan forward from pattern for a number
			rest := line[idx+len(pattern):]
			rest = strings.TrimLeft(rest, " :.\t")
			// Remove commas from number formatting (1,000 -> 1000)
			numStr := ""
			foundDot := false
			for _, c := range rest {
				if c >= '0' && c <= '9' {
					numStr += string(c)
				} else if c == '.' && !foundDot {
					numStr += "."
					foundDot = true
				} else if c == ',' {
					continue // skip commas
				} else if len(numStr) > 0 {
					break
				}
			}
			if len(numStr) > 0 {
				if val, err := strconv.ParseFloat(numStr, 64); err == nil && val > 0 {
					extractedAmount = val
					break
				}
			}
		}
		if extractedAmount > 0 {
			break
		}
	}

	// Fallback: look for any standalone number that looks like an amount (₹100 - ₹100000 range)
	if extractedAmount == 0 {
		words := strings.Fields(req.Text)
		for _, w := range words {
			clean := strings.ReplaceAll(w, ",", "")
			clean = strings.TrimLeft(clean, "₹$")
			if val, err := strconv.ParseFloat(clean, 64); err == nil && val >= 100 && val <= 100000 {
				extractedAmount = val
				break
			}
		}
	}

	if utr == "" {
		writeJSON(w, 200, map[string]interface{}{
			"found":            false,
			"message":          "Could not extract UTR from the provided text. Please enter manually.",
			"extracted_amount": extractedAmount,
		})
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"found":            true,
		"utr":              utr,
		"extracted_amount": extractedAmount,
	})
}

// POST /api/v1/deposit/request — Player requests a deposit
func handleDepositRequest(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role != "client" {
		writeErr(w, 403, "only players can request deposits")
		return
	}

	var req struct {
		Amount    float64 `json:"amount"`
		AccountID int64   `json:"account_id"` // Client can pick account from available-accounts
		UTR       string  `json:"utr"`        // UTR from screenshot extraction
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.Amount < 100 {
		writeErr(w, 400, "minimum deposit is ₹100")
		return
	}
	if req.Amount > dailyLimitINR {
		writeErr(w, 400, fmt.Sprintf("maximum single deposit is ₹%.0f", dailyLimitINR))
		return
	}

	// Find available account — use client's choice or auto-select
	var acct *BankAccount
	var err error
	if req.AccountID > 0 {
		depositStore.mu.RLock()
		a, ok := depositStore.bankAccounts[req.AccountID]
		depositStore.mu.RUnlock()
		if !ok || a.Status != "active" {
			writeErr(w, 400, "selected account not available")
			return
		}
		acct = a
	} else {
		acct, err = depositStore.SelectAvailableAccount(uid, req.Amount, store)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}

	// Find player's agent and master
	player := store.GetUser(uid)
	agentID := int64(0)
	masterID := int64(0)
	if player.ParentID != nil {
		agentID = *player.ParentID
		agent := store.GetUser(agentID)
		if agent != nil && agent.ParentID != nil {
			masterID = *agent.ParentID
		}
	}

	// Create deposit request
	depReq := depositStore.CreateDepositRequest(uid, agentID, masterID, acct.ID, req.Amount)

	// Attach UTR if provided (client extracted from screenshot)
	if req.UTR != "" {
		depositStore.mu.Lock()
		depReq.TxnReference = req.UTR
		depositStore.mu.Unlock()
	}

	// Return account details + QR for player to pay
	writeJSON(w, 200, map[string]interface{}{
		"deposit_id": depReq.ID,
		"amount":     req.Amount,
		"status":     "pending",
		"utr":        req.UTR,
		"pay_to": map[string]interface{}{
			"bank_name":      acct.BankName,
			"account_holder": acct.AccountHolder,
			"account_number": acct.AccountNumber,
			"ifsc_code":      acct.IFSCCode,
			"upi_id":         acct.UPIID,
			"qr_image_url":   acct.QRImageURL,
		},
		"message": "Deposit request submitted. Your Agent will verify and credit your wallet.",
	})

	// Notify hierarchy about new deposit request
	store.NotifyHierarchy(uid, "deposit_request",
		fmt.Sprintf("New deposit request — ₹%.0f", req.Amount),
		fmt.Sprintf("%s requested a deposit of ₹%.0f. Please verify and confirm.", player.Username, req.Amount))
}

// POST /api/v1/deposit/confirm — Agent/Master confirms a deposit
func handleConfirmDeposit(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "only agents/masters can confirm deposits")
		return
	}

	var req struct {
		DepositID int64 `json:"deposit_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.DepositID <= 0 {
		writeErr(w, 400, "deposit_id is required")
		return
	}

	// UTR already attached by player at request time — agent just confirms
	depositStore.mu.RLock()
	existingUTR := ""
	if d, ok := depositStore.depositRequests[req.DepositID]; ok {
		existingUTR = d.TxnReference
	}
	depositStore.mu.RUnlock()

	dep, err := depositStore.ConfirmDeposit(req.DepositID, uid, existingUTR, store)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	// Notify player about deposit confirmation
	store.AddNotification(dep.PlayerID, "deposit",
		fmt.Sprintf("Deposit Confirmed — ₹%.0f", dep.Amount),
		fmt.Sprintf("Your deposit of ₹%.0f has been confirmed and credited to your wallet", dep.Amount))
	// Notify hierarchy
	playerName := ""
	if p := store.GetUser(dep.PlayerID); p != nil {
		playerName = p.Username
	}
	store.NotifyHierarchy(dep.PlayerID, "deposit",
		fmt.Sprintf("%s deposit confirmed", playerName),
		fmt.Sprintf("Deposit of ₹%.0f confirmed for %s", dep.Amount, playerName))

	writeJSON(w, 200, map[string]interface{}{
		"message":    "deposit confirmed and wallet credited",
		"deposit":    dep,
	})
}

// POST /api/v1/deposit/reject — Agent/Master rejects a deposit
func handleRejectDeposit(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "only agents/masters can reject deposits")
		return
	}

	var req struct {
		DepositID int64  `json:"deposit_id"`
		Reason    string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	dep, err := depositStore.RejectDeposit(req.DepositID, uid, req.Reason)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, dep)
}

// GET /api/v1/deposit/requests — Get deposit requests (filtered by role)
func handleGetDepositRequests(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	status := r.URL.Query().Get("status")

	reqs := depositStore.GetDepositRequests(uid, role, status)
	if reqs == nil {
		reqs = []*DepositRequest{}
	}
	// Enrich with player username
	for _, req := range reqs {
		if p := store.GetUser(req.PlayerID); p != nil {
			req.PlayerUsername = p.Username
		}
	}
	writeJSON(w, 200, reqs)
}

// GET /api/v1/deposit/accounts — Get bank accounts (filtered by role)
func handleGetBankAccounts(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)

	var accounts []*BankAccount
	switch role {
	case "superadmin":
		accounts = depositStore.GetAllBankAccounts()
	case "admin", "master":
		accounts = depositStore.GetBankAccountsForMaster(uid, store)
	case "agent":
		accounts = depositStore.GetBankAccountsByOwner(uid)
	default:
		writeErr(w, 403, "not authorized")
		return
	}

	if accounts == nil {
		accounts = []*BankAccount{}
	}
	writeJSON(w, 200, accounts)
}

// POST /api/v1/deposit/accounts — Create a bank account
func handleCreateBankAccount(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role != "master" && role != "agent" && role != "admin" && role != "superadmin" {
		writeErr(w, 403, "only masters/agents can create bank accounts")
		return
	}

	var req BankAccount
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	// Validate required fields
	if req.BankName == "" || req.AccountHolder == "" || req.AccountNumber == "" || req.IFSCCode == "" {
		writeErr(w, 400, "bank_name, account_holder, account_number, and ifsc_code are required")
		return
	}
	if len(req.IFSCCode) != 11 {
		writeErr(w, 400, "IFSC code must be exactly 11 characters")
		return
	}

	ownerRole := role
	if ownerRole == "admin" || ownerRole == "superadmin" {
		ownerRole = "master"
	}

	acct := depositStore.CreateBankAccount(uid, ownerRole, &req)
	store.AddAudit(uid, "", "bank_account_created", fmt.Sprintf("id=%d bank=%s", acct.ID, acct.BankName), "")

	writeJSON(w, 201, acct)
}

// PUT /api/v1/deposit/accounts/{id} — Update a bank account
func handleUpdateBankAccount(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)

	var updates map[string]string
	json.NewDecoder(r.Body).Decode(&updates)

	acct, err := depositStore.UpdateBankAccount(id, uid, updates)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, acct)
}

// DELETE /api/v1/deposit/accounts/{id} — Deactivate a bank account
func handleDeleteBankAccount(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)

	if err := depositStore.DeleteBankAccount(id, uid); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"message": "bank account deactivated"})
}

// GET /api/v1/deposit/usage — Usage dashboard (supports ?owner_id filter for SA)
func handleGetUsageDashboard(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)

	// SuperAdmin can filter by owner_id (admin/master/agent)
	filterOwner := r.URL.Query().Get("owner_id")

	var accounts []*BankAccount
	switch role {
	case "superadmin":
		if filterOwner != "" {
			ownerID, _ := strconv.ParseInt(filterOwner, 10, 64)
			if ownerID > 0 {
				accounts = depositStore.GetBankAccountsForMaster(ownerID, store)
			} else {
				accounts = depositStore.GetAllBankAccounts()
			}
		} else {
			accounts = depositStore.GetAllBankAccounts()
		}
	case "admin", "master":
		accounts = depositStore.GetBankAccountsForMaster(uid, store)
	case "agent":
		accounts = depositStore.GetBankAccountsByOwner(uid)
	default:
		writeErr(w, 403, "not authorized")
		return
	}

	// Build summary
	var totalUsed, totalRemaining float64
	var totalDeposits int
	for _, a := range accounts {
		totalUsed += a.UsedToday
		totalRemaining += a.RemainingLimit
		totalDeposits += a.DepositCount
	}

	writeJSON(w, 200, map[string]interface{}{
		"date":            todayIST(),
		"total_accounts":  len(accounts),
		"total_used":      totalUsed,
		"total_remaining": totalRemaining,
		"total_deposits":  totalDeposits,
		"accounts":        accounts,
	})
}

// ── Helpers ─────────────────────────────────────────────────────

// ── Tree View — full hierarchy with deposit stats per node ───

type TreeNode struct {
	ID             int64       `json:"id"`
	Username       string      `json:"username"`
	Role           string      `json:"role"`
	Balance        float64     `json:"balance"`
	AccountCount   int         `json:"account_count"`
	TotalUsedToday float64     `json:"total_used_today"`
	TotalRemaining float64     `json:"total_remaining"`
	TotalLimit     float64     `json:"total_limit"`
	DepositsPending int        `json:"deposits_pending"`
	DepositsToday   int        `json:"deposits_today"`
	Children       []*TreeNode `json:"children,omitempty"`
}

func (ds *DepositStore) BuildTree(rootID int64, mainStore *Store) *TreeNode {
	user := mainStore.GetUser(rootID)
	if user == nil {
		return nil
	}

	node := &TreeNode{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
		Balance:  user.Balance,
	}

	// Count bank accounts and usage for this user
	ds.mu.RLock()
	today := todayIST()
	for _, acct := range ds.bankAccounts {
		if acct.OwnerID == rootID {
			node.AccountCount++
			node.TotalLimit += acct.DailyLimit
			key := usageKey(acct.ID, today)
			if u, ok := ds.dailyUsage[key]; ok {
				node.TotalUsedToday += u.TotalUsed
				node.DepositsToday += u.DepositCount
			}
			node.TotalRemaining += math.Max(acct.DailyLimit - node.TotalUsedToday, 0)
		}
	}
	// Fix remaining calc (per account, not cumulative)
	node.TotalRemaining = node.TotalLimit - node.TotalUsedToday
	if node.TotalRemaining < 0 { node.TotalRemaining = 0 }

	// Count pending deposits
	for _, req := range ds.depositRequests {
		if req.Status == "pending" && (req.AgentID == rootID || req.MasterID == rootID) {
			node.DepositsPending++
		}
	}
	ds.mu.RUnlock()

	// Recurse into direct children (including players)
	directChildren := mainStore.GetDirectChildren(rootID)
	for _, child := range directChildren {
		if child.Role == "client" {
			// Players: show as leaf node with their deposit stats
			playerNode := &TreeNode{
				ID:       child.ID,
				Username: child.Username,
				Role:     child.Role,
				Balance:  child.Balance,
			}
			// Count player's deposits
			ds.mu.RLock()
			for _, req := range ds.depositRequests {
				if req.PlayerID == child.ID {
					if req.Status == "pending" {
						playerNode.DepositsPending++
					}
					if strings.HasPrefix(req.CreatedAt, todayIST()[:10]) {
						playerNode.DepositsToday++
						if req.Status == "confirmed" {
							playerNode.TotalUsedToday += req.Amount
						}
					}
				}
			}
			ds.mu.RUnlock()
			node.Children = append(node.Children, playerNode)
			continue
		}
		childNode := ds.BuildTree(child.ID, mainStore)
		if childNode != nil {
			node.Children = append(node.Children, childNode)
			// Aggregate child stats up
			node.AccountCount += childNode.AccountCount
			node.TotalUsedToday += childNode.TotalUsedToday
			node.TotalRemaining += childNode.TotalRemaining
			node.TotalLimit += childNode.TotalLimit
			node.DepositsToday += childNode.DepositsToday
			node.DepositsPending += childNode.DepositsPending
		}
	}

	return node
}

// GET /api/v1/deposit/tree — Full hierarchy with deposit stats
func handleDepositTree(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)

	if role == "client" {
		writeErr(w, 403, "not authorized")
		return
	}

	var rootID int64
	if role == "superadmin" {
		// Build tree for each top-level admin
		var roots []*TreeNode
		allUsers := store.AllUsers()
		for _, u := range allUsers {
			if u.Role == "superadmin" {
				tree := depositStore.BuildTree(u.ID, store)
				if tree != nil {
					roots = append(roots, tree)
				}
			}
		}
		if len(roots) == 0 {
			// Fallback: build from the requesting user
			tree := depositStore.BuildTree(uid, store)
			roots = append(roots, tree)
		}
		writeJSON(w, 200, roots)
		return
	}

	rootID = uid
	tree := depositStore.BuildTree(rootID, store)
	if tree == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, tree)
}

// maskAccountNumber returns last 4 digits
func maskAccountNumber(acctNum string) string {
	if len(acctNum) <= 4 {
		return acctNum
	}
	return strings.Repeat("*", len(acctNum)-4) + acctNum[len(acctNum)-4:]
}

// ── DB helper functions for deposit persistence ─────────────────

// dbSaveBankAccount inserts a bank account and returns the DB-assigned ID.
func dbSaveBankAccount(acct *BankAccount) int64 {
	var id int64
	err := db.QueryRow(`
		INSERT INTO betting.bank_accounts
			(owner_id, owner_role, bank_name, account_holder, account_number, ifsc_code, upi_id, qr_image_url, daily_limit, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
		RETURNING id`,
		acct.OwnerID, acct.OwnerRole, acct.BankName, acct.AccountHolder,
		acct.AccountNumber, acct.IFSCCode, acct.UPIID, acct.QRImageURL,
		acct.DailyLimit, acct.Status,
	).Scan(&id)
	if err != nil {
		logger.Error("dbSaveBankAccount failed", "error", err)
		return 0
	}
	return id
}

// dbUpdateBankAccount updates an existing bank account row.
func dbUpdateBankAccount(acct *BankAccount) {
	_, err := db.Exec(`
		UPDATE betting.bank_accounts
		SET bank_name=$1, account_holder=$2, account_number=$3, ifsc_code=$4,
		    upi_id=$5, qr_image_url=$6, status=$7, updated_at=NOW()
		WHERE id=$8`,
		acct.BankName, acct.AccountHolder, acct.AccountNumber, acct.IFSCCode,
		acct.UPIID, acct.QRImageURL, acct.Status, acct.ID,
	)
	if err != nil {
		logger.Error("dbUpdateBankAccount failed", "id", acct.ID, "error", err)
	}
}

// dbGetBankAccountsByOwner loads bank accounts for a given owner from the DB.
func dbGetBankAccountsByOwner(ownerID int64) []*BankAccount {
	rows, err := db.Query(`
		SELECT id, owner_id, owner_role, bank_name, account_holder, account_number,
		       ifsc_code, upi_id, qr_image_url, daily_limit, status, created_at, updated_at
		FROM betting.bank_accounts WHERE owner_id=$1 ORDER BY id`, ownerID)
	if err != nil {
		logger.Error("dbGetBankAccountsByOwner failed", "error", err)
		return nil
	}
	defer rows.Close()
	return scanBankAccountRows(rows)
}

// dbGetAllBankAccounts loads all bank accounts from the DB.
func dbGetAllBankAccounts() []*BankAccount {
	rows, err := db.Query(`
		SELECT id, owner_id, owner_role, bank_name, account_holder, account_number,
		       ifsc_code, upi_id, qr_image_url, daily_limit, status, created_at, updated_at
		FROM betting.bank_accounts ORDER BY id`)
	if err != nil {
		logger.Error("dbGetAllBankAccounts failed", "error", err)
		return nil
	}
	defer rows.Close()
	return scanBankAccountRows(rows)
}

// scanBankAccountRows scans rows into BankAccount slices.
func scanBankAccountRows(rows *sql.Rows) []*BankAccount {
	var out []*BankAccount
	for rows.Next() {
		a := &BankAccount{}
		var upi, qr sql.NullString
		rows.Scan(&a.ID, &a.OwnerID, &a.OwnerRole, &a.BankName, &a.AccountHolder,
			&a.AccountNumber, &a.IFSCCode, &upi, &qr, &a.DailyLimit, &a.Status,
			&a.CreatedAt, &a.UpdatedAt)
		if upi.Valid {
			a.UPIID = upi.String
		}
		if qr.Valid {
			a.QRImageURL = qr.String
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		logger.Error("scanBankAccountRows iteration error", "error", err)
	}
	return out
}

// dbSaveDepositRequest inserts a deposit request and returns the DB-assigned ID.
func dbSaveDepositRequest(req *DepositRequest) int64 {
	var id int64
	err := db.QueryRow(`
		INSERT INTO betting.deposit_requests
			(player_id, agent_id, master_id, bank_account_id, amount, status, txn_reference, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW(),NOW())
		RETURNING id`,
		req.PlayerID, req.AgentID, req.MasterID, req.BankAccountID,
		req.Amount, req.Status, req.TxnReference,
	).Scan(&id)
	if err != nil {
		logger.Error("dbSaveDepositRequest failed", "error", err)
		return 0
	}
	return id
}

// dbUpdateDepositRequest updates status, confirmed_by, confirmed_at, txn_reference, rejection_reason.
func dbUpdateDepositRequest(req *DepositRequest) {
	var confirmedBy sql.NullInt64
	var confirmedAt sql.NullString
	if req.ConfirmedBy > 0 {
		confirmedBy = sql.NullInt64{Int64: req.ConfirmedBy, Valid: true}
	}
	if req.ConfirmedAt != "" {
		confirmedAt = sql.NullString{String: req.ConfirmedAt, Valid: true}
	}
	_, err := db.Exec(`
		UPDATE betting.deposit_requests
		SET status=$1, confirmed_by=$2, confirmed_at=$3, txn_reference=$4, rejection_reason=$5, updated_at=NOW()
		WHERE id=$6`,
		req.Status, confirmedBy, confirmedAt, req.TxnReference, req.RejectionReason, req.ID,
	)
	if err != nil {
		logger.Error("dbUpdateDepositRequest failed", "id", req.ID, "error", err)
	}
}

// dbGetDepositRequests loads deposit requests from DB filtered by role and status.
func dbGetDepositRequests(userID int64, role string, statusFilter string) []*DepositRequest {
	var query string
	var args []interface{}

	switch role {
	case "superadmin":
		if statusFilter != "" {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE status=$1 ORDER BY id DESC`
			args = []interface{}{statusFilter}
		} else {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests ORDER BY id DESC`
		}
	case "admin", "master":
		if statusFilter != "" {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE (master_id=$1 OR agent_id=$1) AND status=$2 ORDER BY id DESC`
			args = []interface{}{userID, statusFilter}
		} else {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE master_id=$1 OR agent_id=$1 ORDER BY id DESC`
			args = []interface{}{userID}
		}
	case "agent":
		if statusFilter != "" {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE agent_id=$1 AND status=$2 ORDER BY id DESC`
			args = []interface{}{userID, statusFilter}
		} else {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE agent_id=$1 ORDER BY id DESC`
			args = []interface{}{userID}
		}
	case "client":
		if statusFilter != "" {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE player_id=$1 AND status=$2 ORDER BY id DESC`
			args = []interface{}{userID, statusFilter}
		} else {
			query = `SELECT id, player_id, agent_id, master_id, bank_account_id, amount, status,
			         confirmed_by, confirmed_at, txn_reference, rejection_reason, created_at
			         FROM betting.deposit_requests WHERE player_id=$1 ORDER BY id DESC`
			args = []interface{}{userID}
		}
	default:
		return nil
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		logger.Error("dbGetDepositRequests failed", "error", err)
		return nil
	}
	defer rows.Close()

	var out []*DepositRequest
	for rows.Next() {
		r := &DepositRequest{}
		var confirmedBy sql.NullInt64
		var confirmedAt, txnRef, rejReason sql.NullString
		rows.Scan(&r.ID, &r.PlayerID, &r.AgentID, &r.MasterID, &r.BankAccountID,
			&r.Amount, &r.Status, &confirmedBy, &confirmedAt, &txnRef, &rejReason, &r.CreatedAt)
		if confirmedBy.Valid {
			r.ConfirmedBy = confirmedBy.Int64
		}
		if confirmedAt.Valid {
			r.ConfirmedAt = confirmedAt.String
		}
		if txnRef.Valid {
			r.TxnReference = txnRef.String
		}
		if rejReason.Valid {
			r.RejectionReason = rejReason.String
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetDepositRequests rows iteration error", "error", err)
	}
	return out
}

// dbSaveDailyUsage upserts the daily usage record for a bank account.
func dbSaveDailyUsage(usage *DailyUsage) {
	_, err := db.Exec(`
		INSERT INTO betting.daily_account_usage (bank_account_id, usage_date, total_used, deposit_count, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (bank_account_id, usage_date)
		DO UPDATE SET total_used=$3, deposit_count=$4, updated_at=NOW()`,
		usage.BankAccountID, usage.UsageDate, usage.TotalUsed, usage.DepositCount,
	)
	if err != nil {
		logger.Error("dbSaveDailyUsage failed", "account", usage.BankAccountID, "error", err)
	}
}

// dbGetDailyUsage reads today's usage for a bank account from DB.
func dbGetDailyUsage(accountID int64, date string) (float64, int) {
	var totalUsed float64
	var depositCount int
	err := db.QueryRow(`
		SELECT total_used, deposit_count FROM betting.daily_account_usage
		WHERE bank_account_id=$1 AND usage_date=$2`, accountID, date).Scan(&totalUsed, &depositCount)
	if err != nil {
		return 0, 0
	}
	return totalUsed, depositCount
}
