package admin

import "github.com/lotus-exchange/lotus-exchange/pkg/httputil"

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

// SeedResult is the JSON payload returned by POST /api/v1/seed. It lists
// every step the endpoint took so the integration test suite can verify
// exactly which users were created and which credit transfers landed.
type SeedResult struct {
	Users   []string `json:"users"`
	Credits []string `json:"credits"`
	Bets    []string `json:"bets"`
}

// RegisterSeedRoute registers the dev-only seed bootstrap endpoint. It is
// intentionally registered as a PUBLIC route (no auth middleware) because
// the integration test suite must be able to call it before any user
// exists. Access control is provided by the X-Seed-Secret header which
// must match the SEED_SECRET environment variable.
//
// When SEED_SECRET is unset the endpoint returns 404 so that production
// deployments cannot accidentally expose it.
func (h *Handler) RegisterSeedRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/seed", h.Seed)
}

// Seed creates the standard superadmin → admin → master → agent → player
// hierarchy, funds each tier via credit transfers, and places a handful of
// sample bets so the order book has some depth for downstream tests.
func (h *Handler) Seed(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("SEED_SECRET")
	if secret == "" {
		http.Error(w, `{"error":"seed endpoint disabled"}`, http.StatusNotFound)
		return
	}
	if r.Header.Get("X-Seed-Secret") != secret {
		http.Error(w, `{"error":"invalid seed secret"}`, http.StatusUnauthorized)
		return
	}
	if h.auth == nil || h.wallet == nil {
		http.Error(w, `{"error":"seed not wired: auth/wallet services missing"}`, http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	res := SeedResult{
		Users:   []string{},
		Credits: []string{},
		Bets:    []string{},
	}

	// Defaults chosen to satisfy the auth service's password complexity
	// rules (min 8, one upper, one lower, one digit).
	pwSuperadmin := envOr("SEED_SUPERADMIN_PASSWORD", "Admin@123")
	pwAdmin := envOr("SEED_ADMIN_PASSWORD", "Admin@123")
	pwMaster := envOr("SEED_MASTER_PASSWORD", "Master@123")
	pwAgent := envOr("SEED_AGENT_PASSWORD", "Agent@123")
	pwPlayer := envOr("SEED_PLAYER_PASSWORD", "Player@123")

	type seedUser struct {
		Username string
		Email    string
		Password string
		Role     models.Role
		// ParentName references an earlier entry in this list. Empty for
		// the root superadmin.
		ParentName     string
		CreditLimit    float64
		CommissionRate float64
	}

	plan := []seedUser{
		{"superadmin", "sa@lotus.com", pwSuperadmin, models.RoleSuperAdmin, "", 10000000, 5},
		{"admin1", "ad@lotus.com", pwAdmin, models.RoleAdmin, "superadmin", 5000000, 4},
		{"master1", "ma@lotus.com", pwMaster, models.RoleMaster, "admin1", 1000000, 3},
		{"agent1", "ag@lotus.com", pwAgent, models.RoleAgent, "master1", 500000, 2},
		{"player1", "p1@lotus.com", pwPlayer, models.RoleClient, "agent1", 100000, 2},
		{"player2", "p2@lotus.com", pwPlayer, models.RoleClient, "agent1", 100000, 2},
	}

	created := map[string]int64{}

	for _, u := range plan {
		// Idempotency: if the user already exists, reuse its id rather
		// than failing. This lets the integration test suite re-run the
		// seed endpoint against a warm database.
		if existingID, ok := lookupUserID(ctx, h.db, u.Username); ok {
			created[u.Username] = existingID
			res.Users = append(res.Users, fmt.Sprintf("%s: id=%d (existing)", u.Username, existingID))
			continue
		}

		var parentPtr *int64
		if u.ParentName != "" {
			pid, ok := created[u.ParentName]
			if !ok {
				res.Users = append(res.Users, fmt.Sprintf("%s: missing parent %s", u.Username, u.ParentName))
				continue
			}
			parentPtr = &pid
		}

		user, err := h.auth.Register(ctx, &models.CreateUserRequest{
			Username:       u.Username,
			Email:          u.Email,
			Password:       u.Password,
			Role:           u.Role,
			ParentID:       parentPtr,
			CreditLimit:    u.CreditLimit,
			CommissionRate: u.CommissionRate,
		})
		if err != nil {
			res.Users = append(res.Users, fmt.Sprintf("%s: %v", u.Username, err))
			continue
		}
		created[u.Username] = user.ID
		res.Users = append(res.Users, fmt.Sprintf("%s: id=%d", u.Username, user.ID))
	}

	// Bootstrap: give the superadmin a balance to push through the chain.
	// Credit transfers debit the sender, so without a starting balance
	// the cascade fails at the first hop. We skip this step silently if
	// the superadmin row wasn't created.
	if saID, ok := created["superadmin"]; ok {
		ref := fmt.Sprintf("seed-bootstrap:%d", saID)
		if err := h.wallet.Deposit(ctx, saID, 10_000_000, ref); err != nil {
			res.Credits = append(res.Credits, fmt.Sprintf("bootstrap deposit: %v", err))
		} else {
			res.Credits = append(res.Credits, "bootstrap deposit: superadmin += 10,000,000")
		}
	}

	// Credit cascade: each step must respect hierarchy.TransferCredit's
	// parent-child check, which is why we move money one level at a time.
	transfers := []struct {
		from, to string
		amount   float64
	}{
		{"superadmin", "admin1", 500000},
		{"admin1", "master1", 200000},
		{"master1", "agent1", 100000},
		{"agent1", "player1", 50000},
		{"agent1", "player2", 50000},
	}

	for _, t := range transfers {
		fromID, fromOk := created[t.from]
		toID, toOk := created[t.to]
		if !fromOk || !toOk {
			res.Credits = append(res.Credits, fmt.Sprintf("%s→%s: missing user", t.from, t.to))
			continue
		}
		if err := h.transferCredit(ctx, fromID, toID, t.amount); err != nil {
			res.Credits = append(res.Credits, fmt.Sprintf("%s→%s: %v", t.from, t.to, err))
			continue
		}
		res.Credits = append(res.Credits,
			fmt.Sprintf("%s→%s: %.0f OK", t.from, t.to, t.amount))
	}

	// Sample bets so the order book has some depth. Failures are
	// non-fatal because the matching engine may not be available in
	// every deployment (e.g. unit tests) — we just record what happened.
	p1ID := created["player1"]
	p2ID := created["player2"]
	if p1ID > 0 && p2ID > 0 {
		res.Bets = append(res.Bets, h.placeSampleBets(ctx, p1ID, p2ID)...)
	}

	httputil.WriteJSON(w, http.StatusOK, res)
}

// transferCredit moves money between two accounts. If the hierarchy
// service is available it is used (so the transfer shows up in ledger
// entries exactly like a real credit transfer). Otherwise we fall back
// to a plain SQL update so the seed still works on a bare-bones DB.
func (h *Handler) transferCredit(ctx context.Context, fromID, toID int64, amount float64) error {
	if h.hierarchy != nil {
		return h.hierarchy.TransferCredit(ctx, &models.CreditTransferRequest{
			FromUserID: fromID,
			ToUserID:   toID,
			Amount:     amount,
		})
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET balance = balance - $1, updated_at = NOW() WHERE id = $2`,
		amount, fromID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2`,
		amount, toID); err != nil {
		return err
	}
	return tx.Commit()
}

// placeSampleBets inserts a few rows into betting.bets so the order book
// has something to display. We write directly to the DB rather than going
// through the matching engine because the seed endpoint needs to be
// deterministic and idempotent and must not depend on the engine's Redis
// state being reachable.
func (h *Handler) placeSampleBets(ctx context.Context, p1ID, p2ID int64) []string {
	var out []string

	samples := []struct {
		userID      int64
		marketID    string
		selectionID int64
		side        string
		price       float64
		stake       float64
		clientRef   string
	}{
		{p1ID, "ipl-mi-csk-mo", 101, "back", 1.80, 5000, "seed-1"},
		{p1ID, "ipl-mi-csk-mo", 101, "back", 1.75, 3000, "seed-2"},
		{p2ID, "ipl-mi-csk-mo", 101, "lay", 1.90, 6000, "seed-3"},
		{p2ID, "ipl-mi-csk-mo", 101, "lay", 1.95, 4000, "seed-4"},
		{p1ID, "ipl-mi-csk-mo", 102, "back", 2.10, 4000, "seed-5"},
		{p2ID, "ipl-mi-csk-mo", 102, "lay", 2.15, 3000, "seed-6"},
	}

	for _, s := range samples {
		betID := fmt.Sprintf("seed-bet-%s-%d-%d", s.clientRef, s.userID, s.selectionID)
		_, err := h.db.ExecContext(ctx,
			`INSERT INTO bets (id, market_id, selection_id, user_id, side, price, stake,
			                   matched_stake, unmatched_stake, profit, status, client_ref, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $7, 0, 'pending', $8, NOW())
			 ON CONFLICT (id) DO NOTHING`,
			betID, s.marketID, s.selectionID, s.userID, s.side, s.price, s.stake, s.clientRef,
		)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", s.clientRef, err))
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s %.2f @ %.2f", s.clientRef, s.side, s.stake, s.price))
	}
	return out
}

func lookupUserID(ctx context.Context, db *sql.DB, username string) (int64, bool) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
