package admin

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
)

// seedTestHandler builds a Handler with a real auth + wallet service backed
// by the adminfake driver. hierarchy is intentionally left nil so that
// Seed's internal transferCredit falls back to plain SQL UPDATEs — we then
// count those updates via the adminFakeState counters.
func seedTestHandler(t *testing.T) (*Handler, *sql.DB, *adminFakeState) {
	t.Helper()
	db, st := registerAdminFakeDB(t)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authSvc, err := auth.NewService(db, nil, logger, time.Minute, time.Hour, "", "")
	if err != nil {
		t.Fatalf("auth.NewService: %v", err)
	}
	walletSvc := wallet.NewService(db, nil, logger)

	h := &Handler{
		db:     db,
		logger: logger,
		auth:   authSvc,
		wallet: walletSvc,
	}
	return h, db, st
}

// TestSeed_MissingEnvVar_Returns404 verifies that when SEED_SECRET is not
// set, the endpoint reports 404 so production deployments cannot expose it
// by accident.
func TestSeed_MissingEnvVar_Returns404(t *testing.T) {
	t.Setenv("SEED_SECRET", "") // ensure unset in this subprocess
	h, _, _ := seedTestHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/seed", nil)
	rr := httptest.NewRecorder()
	h.Seed(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "disabled") {
		t.Errorf("body should mention 'disabled', got: %s", rr.Body.String())
	}
}

// TestSeed_WrongHeader_Returns401 verifies that with SEED_SECRET set but
// the wrong X-Seed-Secret header, we return 401 Unauthorized.
func TestSeed_WrongHeader_Returns401(t *testing.T) {
	t.Setenv("SEED_SECRET", "super-secret")
	h, _, _ := seedTestHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/seed", nil)
	req.Header.Set("X-Seed-Secret", "wrong")
	rr := httptest.NewRecorder()
	h.Seed(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

// TestSeed_WrongHeader_EmptyHeader_Returns401 — missing header is still a
// caller-error, not a "disabled" response.
func TestSeed_WrongHeader_EmptyHeader_Returns401(t *testing.T) {
	t.Setenv("SEED_SECRET", "super-secret")
	h, _, _ := seedTestHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/seed", nil)
	rr := httptest.NewRecorder()
	h.Seed(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

// TestSeed_MissingDeps_Returns500 — when SEED_SECRET is set and the secret
// matches but the auth/wallet services are not wired, the endpoint must
// return 500 rather than panicking with a nil-pointer dereference.
func TestSeed_MissingDeps_Returns500(t *testing.T) {
	t.Setenv("SEED_SECRET", "super-secret")
	db, _ := registerAdminFakeDB(t)
	h := &Handler{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// auth/wallet intentionally nil
	}

	req := httptest.NewRequest("POST", "/api/v1/seed", nil)
	req.Header.Set("X-Seed-Secret", "super-secret")
	rr := httptest.NewRecorder()
	h.Seed(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

// TestSeed_HappyPath_CreatesUsers exercises the full seed bootstrap: it
// registers the 6-user hierarchy, deposits seed credit, cascades credit
// transfers, and inserts sample bets. We verify the response and — via the
// fake DB counters — that Register, Deposit, and the credit-transfer
// fallback were all invoked.
func TestSeed_HappyPath_CreatesUsers(t *testing.T) {
	t.Setenv("SEED_SECRET", "letmein")
	h, _, st := seedTestHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/seed", nil)
	req.Header.Set("X-Seed-Secret", "letmein")
	rr := httptest.NewRecorder()
	h.Seed(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var res SeedResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	// Response shape: all six users reported, no credit-transfer errors.
	if len(res.Users) != 6 {
		t.Fatalf("res.Users = %d entries, want 6: %v", len(res.Users), res.Users)
	}
	expectedNames := []string{"superadmin", "admin1", "master1", "agent1", "player1", "player2"}
	for _, name := range expectedNames {
		found := false
		for _, entry := range res.Users {
			if strings.HasPrefix(entry, name+":") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing user entry for %s: %v", name, res.Users)
		}
	}

	// All seed users should have been registered via auth.Register (which
	// feeds INSERT INTO users RETURNING into our fake).
	if got := atomic.LoadInt64(&st.registerInsertCalls); got != 6 {
		t.Errorf("registerInsertCalls = %d, want 6", got)
	}
	// All 6 users should now exist in the fake state.
	st.mu.Lock()
	nUsers := len(st.users)
	st.mu.Unlock()
	if nUsers != 6 {
		t.Errorf("fake state has %d users, want 6", nUsers)
	}

	// Superadmin deposit + 5 transfer-credit cascades should each move
	// balance. The fallback transferCredit performs one debit + one credit
	// per hop, giving 5 debits. wallet.Deposit adds one more credit.
	if got := atomic.LoadInt64(&st.depositCalls); got < 1 {
		t.Errorf("depositCalls = %d, want >= 1 (bootstrap deposit)", got)
	}
	if got := atomic.LoadInt64(&st.balanceDebits); got < 5 {
		t.Errorf("balanceDebits = %d, want >= 5 (one per transfer hop)", got)
	}
	if got := atomic.LoadInt64(&st.balanceCredits); got < 5 {
		t.Errorf("balanceCredits = %d, want >= 5 (deposit + per-hop credit)", got)
	}

	// Sample bets: 6 inserts into the bets table.
	if got := atomic.LoadInt64(&st.sampleBetInserts); got != 6 {
		t.Errorf("sampleBetInserts = %d, want 6", got)
	}
	if len(res.Bets) != 6 {
		t.Errorf("res.Bets = %d, want 6: %v", len(res.Bets), res.Bets)
	}

	// Every credit line should report OK (no error substring).
	for _, c := range res.Credits {
		if strings.Contains(c, "missing user") || strings.Contains(c, "error") {
			t.Errorf("credit line reported failure: %s", c)
		}
	}
}

// TestSeed_Idempotent verifies the endpoint can be re-run against a warm
// database: subsequent calls detect existing users and short-circuit rather
// than producing duplicate-key errors.
func TestSeed_Idempotent(t *testing.T) {
	t.Setenv("SEED_SECRET", "letmein")
	h, _, st := seedTestHandler(t)

	// First call
	req1 := httptest.NewRequest("POST", "/api/v1/seed", nil)
	req1.Header.Set("X-Seed-Secret", "letmein")
	rr1 := httptest.NewRecorder()
	h.Seed(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first call: status = %d, body=%s", rr1.Code, rr1.Body.String())
	}

	firstRegisterCalls := atomic.LoadInt64(&st.registerInsertCalls)
	if firstRegisterCalls != 6 {
		t.Fatalf("first call: registerInsertCalls = %d, want 6", firstRegisterCalls)
	}

	// Second call — should succeed and NOT register any new users.
	req2 := httptest.NewRequest("POST", "/api/v1/seed", nil)
	req2.Header.Set("X-Seed-Secret", "letmein")
	rr2 := httptest.NewRecorder()
	h.Seed(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("second call: status = %d, body=%s", rr2.Code, rr2.Body.String())
	}

	var res2 SeedResult
	if err := json.Unmarshal(rr2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("decode second body: %v", err)
	}

	// All 6 user entries should be reported as existing.
	if len(res2.Users) != 6 {
		t.Fatalf("second call: res.Users = %d, want 6", len(res2.Users))
	}
	for _, entry := range res2.Users {
		if !strings.Contains(entry, "(existing)") {
			t.Errorf("second call: expected (existing) marker on %q", entry)
		}
	}

	// Critically: NO additional Register calls on the second run.
	if got := atomic.LoadInt64(&st.registerInsertCalls); got != firstRegisterCalls {
		t.Errorf("second call triggered %d extra Register calls (total now %d), want 0",
			got-firstRegisterCalls, got)
	}

	// The fake state must still contain exactly 6 users (no duplicates).
	st.mu.Lock()
	nUsers := len(st.users)
	st.mu.Unlock()
	if nUsers != 6 {
		t.Errorf("fake state has %d users after idempotent re-run, want 6", nUsers)
	}
}
