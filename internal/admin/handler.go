package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/fraud"
	"github.com/lotus-exchange/lotus-exchange/internal/hierarchy"
	"github.com/lotus-exchange/lotus-exchange/internal/market"
	"github.com/lotus-exchange/lotus-exchange/internal/matching"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/reporting"
	"github.com/lotus-exchange/lotus-exchange/internal/settlement"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/lotus-exchange/lotus-exchange/pkg/httputil"
)

type Handler struct {
	db         *sql.DB
	market     *market.Service
	settlement *settlement.Service
	reporting  *reporting.Service
	fraud      *fraud.Service
	logger     *slog.Logger

	// Optional dependencies used by the admin-service microservice build
	// (seed + panel routes). May be nil when the handler is constructed
	// inside the gateway where these services are wired separately.
	auth      *auth.Service
	wallet    *wallet.Service
	hierarchy *hierarchy.Service
	matching  *matching.Engine
}

func NewHandler(
	db *sql.DB,
	marketSvc *market.Service,
	settlementSvc *settlement.Service,
	reportingSvc *reporting.Service,
	fraudSvc *fraud.Service,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		db:         db,
		market:     marketSvc,
		settlement: settlementSvc,
		reporting:  reportingSvc,
		fraud:      fraudSvc,
		logger:     logger,
	}
}

// WithMicroserviceDeps attaches the extra services that the admin-service
// binary needs to serve the dev seed bootstrap and panel routes. It is a
// no-op for callers (such as the gateway) that do not need those endpoints.
func (h *Handler) WithMicroserviceDeps(
	authSvc *auth.Service,
	walletSvc *wallet.Service,
	hierarchySvc *hierarchy.Service,
	matchingEngine *matching.Engine,
) *Handler {
	h.auth = authSvc
	h.wallet = walletSvc
	h.hierarchy = hierarchySvc
	h.matching = matchingEngine
	return h
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Dashboard
	mux.HandleFunc("GET /api/v1/admin/dashboard", h.Dashboard)

	// User management
	mux.HandleFunc("GET /api/v1/admin/users", h.ListUsers)
	mux.HandleFunc("GET /api/v1/admin/users/{id}", h.GetUser)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/status", h.UpdateUserStatus)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/credit-limit", h.UpdateCreditLimit)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/commission", h.UpdateCommission)

	// Market management
	mux.HandleFunc("GET /api/v1/admin/markets", h.ListMarkets)
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/suspend", h.SuspendMarket)
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/resume", h.ResumeMarket)
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/settle", h.SettleMarket)
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/void", h.VoidMarket)

	// Bets
	mux.HandleFunc("GET /api/v1/admin/bets", h.ListBets)

	// Reports
	mux.HandleFunc("GET /api/v1/admin/reports/pnl", h.PnLReport)
	mux.HandleFunc("GET /api/v1/admin/reports/volume", h.VolumeReport)

	// Fraud
	mux.HandleFunc("GET /api/v1/admin/fraud/alerts", h.FraudAlerts)
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.reporting.GetDashboardStats(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard stats failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load dashboard stats")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, stats)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")
	limit := parseIntParam(r, "limit", 50)
	offset := parseIntParam(r, "offset", 0)

	query := `SELECT id, username, email, role, path, parent_id, balance, exposure,
	          credit_limit, commission_rate, status, created_at, updated_at
	          FROM users WHERE 1=1`
	var args []interface{}
	argIdx := 1

	if role != "" {
		query += " AND role = $" + strconv.Itoa(argIdx)
		args = append(args, role)
		argIdx++
	}
	if status != "" {
		query += " AND status = $" + strconv.Itoa(argIdx)
		args = append(args, status)
		argIdx++
	}

	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(argIdx) + " OFFSET $" + strconv.Itoa(argIdx+1)
	args = append(args, limit, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list users query failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
			&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
			&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			h.logger.ErrorContext(r.Context(), "scan user failed", "error", err)
			httputil.WriteError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		users = append(users, u)
	}

	httputil.WriteJSON(w, http.StatusOK, users)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	u := &models.User{}
	err = h.db.QueryRowContext(r.Context(),
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
		&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
		&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	var req struct {
		Status        string `json:"status"`
		CurrentStatus string `json:"current_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	allowedStatuses := map[string]bool{"active": true, "suspended": true, "locked": true, "closed": true}
	if !allowedStatuses[req.Status] {
		httputil.WriteError(w, http.StatusBadRequest, "invalid status: must be one of active, suspended, locked, closed")
		return
	}

	// Use optimistic locking: only update if current status matches expected value
	var result sql.Result
	if req.CurrentStatus != "" {
		if !allowedStatuses[req.CurrentStatus] {
			httputil.WriteError(w, http.StatusBadRequest, "invalid current_status: must be one of active, suspended, locked, closed")
			return
		}
		result, err = h.db.ExecContext(r.Context(),
			"UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2 AND status = $3",
			req.Status, id, req.CurrentStatus)
	} else {
		result, err = h.db.ExecContext(r.Context(),
			"UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2",
			req.Status, id)
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}
	if rowsAffected == 0 {
		httputil.WriteError(w, http.StatusConflict, "user status was modified by another operation; please retry")
		return
	}

	h.logger.InfoContext(r.Context(), "admin updated user status",
		"admin", middleware.UserIDFromContext(r.Context()), "user", id, "status", req.Status)

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "status updated"})
}

func (h *Handler) UpdateCreditLimit(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	var req struct {
		CreditLimit float64 `json:"credit_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CreditLimit < 0 {
		httputil.WriteError(w, http.StatusBadRequest, "credit limit must not be negative")
		return
	}
	if req.CreditLimit > 100_000_000 {
		httputil.WriteError(w, http.StatusBadRequest, "credit limit exceeds maximum allowed value of 100,000,000")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		"UPDATE users SET credit_limit = $1, updated_at = NOW() WHERE id = $2",
		req.CreditLimit, id)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update credit limit")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "credit limit updated"})
}

func (h *Handler) UpdateCommission(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	var req struct {
		CommissionRate float64 `json:"commission_rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CommissionRate < 0 || req.CommissionRate > 100 {
		httputil.WriteError(w, http.StatusBadRequest, "commission rate must be between 0 and 100")
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		"UPDATE users SET commission_rate = $1, updated_at = NOW() WHERE id = $2",
		req.CommissionRate, id)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update commission rate")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "commission rate updated"})
}

func (h *Handler) ListMarkets(w http.ResponseWriter, r *http.Request) {
	sport := r.URL.Query().Get("sport")
	status := r.URL.Query().Get("status")
	var inPlay *bool
	if ip := r.URL.Query().Get("in_play"); ip != "" {
		b := ip == "true"
		inPlay = &b
	}

	markets, err := h.market.List(r.Context(), sport, status, inPlay)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list markets failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list markets")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, markets)
}

func (h *Handler) SuspendMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.market.UpdateStatus(r.Context(), marketID, models.MarketSuspended); err != nil {
		h.logger.ErrorContext(r.Context(), "suspend market failed", "market", marketID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to suspend market")
		return
	}

	h.logger.InfoContext(r.Context(), "market suspended",
		"admin", middleware.UserIDFromContext(r.Context()), "market", marketID)

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "market suspended"})
}

func (h *Handler) ResumeMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.market.UpdateStatus(r.Context(), marketID, models.MarketOpen); err != nil {
		h.logger.ErrorContext(r.Context(), "resume market failed", "market", marketID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to resume market")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "market resumed"})
}

func (h *Handler) SettleMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	var req struct {
		WinnerSelectionID int64 `json:"winner_selection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.settlement.SettleMarket(r.Context(), marketID, req.WinnerSelectionID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "settle market failed", "market", marketID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to settle market")
		return
	}

	h.logger.InfoContext(r.Context(), "market settled by admin",
		"admin", middleware.UserIDFromContext(r.Context()), "market", marketID, "winner", req.WinnerSelectionID)

	httputil.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) VoidMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.settlement.VoidMarket(r.Context(), marketID); err != nil {
		h.logger.ErrorContext(r.Context(), "void market failed", "market", marketID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to void market")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "market voided"})
}

func (h *Handler) ListBets(w http.ResponseWriter, r *http.Request) {
	marketID := r.URL.Query().Get("market_id")
	userIDStr := r.URL.Query().Get("user_id")
	status := r.URL.Query().Get("status")
	limit := parseIntParam(r, "limit", 50)

	query := `SELECT id, market_id, selection_id, user_id, side, price, stake,
	          matched_stake, unmatched_stake, profit, status, client_ref, created_at
	          FROM bets WHERE 1=1`
	var args []interface{}
	argIdx := 1

	if marketID != "" {
		query += " AND market_id = $" + strconv.Itoa(argIdx)
		args = append(args, marketID)
		argIdx++
	}
	if userIDStr != "" {
		query += " AND user_id = $" + strconv.Itoa(argIdx)
		args = append(args, userIDStr)
		argIdx++
	}
	if status != "" {
		query += " AND status = $" + strconv.Itoa(argIdx)
		args = append(args, status)
		argIdx++
	}
	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(argIdx)
	args = append(args, limit)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list bets query failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list bets")
		return
	}
	defer rows.Close()

	var bets []*models.Bet
	for rows.Next() {
		b := &models.Bet{}
		if err := rows.Scan(&b.ID, &b.MarketID, &b.SelectionID, &b.UserID, &b.Side,
			&b.Price, &b.Stake, &b.MatchedStake, &b.UnmatchedStake, &b.Profit,
			&b.Status, &b.ClientRef, &b.CreatedAt); err != nil {
			h.logger.ErrorContext(r.Context(), "scan bet failed", "error", err)
			httputil.WriteError(w, http.StatusInternalServerError, "failed to list bets")
			return
		}
		bets = append(bets, b)
	}

	httputil.WriteJSON(w, http.StatusOK, bets)
}

func (h *Handler) PnLReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	userIDStr := r.URL.Query().Get("user_id")

	if userIDStr != "" {
		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		report, err := h.reporting.GetUserPnL(r.Context(), userID, from, to)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "user pnl report failed", "user", userID, "error", err)
			httputil.WriteError(w, http.StatusInternalServerError, "failed to generate PnL report")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, report)
		return
	}

	// Hierarchy P&L for the admin
	adminID := middleware.UserIDFromContext(r.Context())
	reports, err := h.reporting.GetHierarchyPnL(r.Context(), adminID, from, to)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "hierarchy pnl report failed", "admin", adminID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to generate PnL report")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, reports)
}

func (h *Handler) VolumeReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	interval := parseIntParam(r, "interval", 15)

	points, err := h.reporting.GetBetVolumeTrend(r.Context(), from, to, interval)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "volume report failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to generate volume report")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, points)
}

// ---------------------------------------------------------------------------
// Panel routes (scoped to caller's downline — agents and masters may call)
// ---------------------------------------------------------------------------

// RegisterPanelRoutes wires up the /api/v1/panel/* endpoints onto the given
// mux. These routes require an authenticated caller but do NOT require an
// admin role — any non-client user (agent, master, admin, superadmin) can
// access them and results are scoped to the caller's downline.
func (h *Handler) RegisterPanelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/panel/dashboard", h.PanelDashboard)
	mux.HandleFunc("GET /api/v1/panel/users", h.PanelUsers)
	mux.HandleFunc("GET /api/v1/panel/audit", h.PanelAudit)
	mux.HandleFunc("GET /api/v1/panel/reports/pnl", h.PanelPnL)
	mux.HandleFunc("GET /api/v1/panel/reports/volume", h.PanelVolume)
}

// PanelDashboard returns an aggregate summary for the calling user:
// balance, exposure, direct-child count, total downline size, and when
// the caller is a superadmin, platform-wide stats.
func (h *Handler) PanelDashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	role := roleFromCtx(r)
	if !panelRoleAllowed(role) {
		httputil.WriteError(w, http.StatusForbidden, "clients cannot access panel")
		return
	}

	stats := map[string]interface{}{
		"user_id": userID,
		"role":    string(role),
	}

	var (
		username string
		balance  float64
		exposure float64
	)
	err := h.db.QueryRowContext(r.Context(),
		`SELECT username, balance, exposure FROM users WHERE id = $1`,
		userID,
	).Scan(&username, &balance, &exposure)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	stats["username"] = username
	stats["own_balance"] = balance
	stats["own_exposure"] = exposure
	stats["available_balance"] = balance - exposure

	// Downline counts via ltree (users.path <@ caller.path, excluding self).
	// Scan errors are tolerated here — these are dashboard counters, not
	// load-bearing data, and any failure simply leaves the zero value.
	var directChildren, downlineTotal int
	_ = h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE parent_id = $1`, userID,
	).Scan(&directChildren)
	_ = h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users
		 WHERE path <@ (SELECT path FROM users WHERE id = $1) AND id != $1`,
		userID,
	).Scan(&downlineTotal)
	stats["direct_children"] = directChildren
	stats["downline_total"] = downlineTotal

	// Downline aggregate balance/exposure.
	var downlineBalance, downlineExposure float64
	_ = h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(balance), 0), COALESCE(SUM(exposure), 0)
		 FROM users
		 WHERE path <@ (SELECT path FROM users WHERE id = $1) AND id != $1`,
		userID,
	).Scan(&downlineBalance, &downlineExposure)
	stats["downline_balance"] = downlineBalance
	stats["downline_exposure"] = downlineExposure

	// Superadmin gets platform-wide stats.
	if role == models.RoleSuperAdmin {
		var totalUsers, totalMarkets, totalBets int
		var totalVolume float64
		_ = h.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users").Scan(&totalUsers)
		_ = h.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM markets").Scan(&totalMarkets)
		_ = h.db.QueryRowContext(r.Context(),
			"SELECT COUNT(*), COALESCE(SUM(stake), 0) FROM bets",
		).Scan(&totalBets, &totalVolume)
		stats["platform_total_users"] = totalUsers
		stats["platform_total_markets"] = totalMarkets
		stats["platform_total_bets"] = totalBets
		stats["platform_total_volume"] = totalVolume
	}

	httputil.WriteJSON(w, http.StatusOK, stats)
}

// PanelUsers returns every user in the caller's downline. SuperAdmin sees
// every user in the system. Supports an optional ?role= filter.
func (h *Handler) PanelUsers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	role := roleFromCtx(r)
	if !panelRoleAllowed(role) {
		httputil.WriteError(w, http.StatusForbidden, "clients cannot access panel")
		return
	}

	filterRole := r.URL.Query().Get("role")
	limit := parseIntParam(r, "limit", 200)
	offset := parseIntParam(r, "offset", 0)

	var (
		rows *sql.Rows
		err  error
	)

	base := `SELECT id, username, email, role, path, parent_id, balance, exposure,
	                credit_limit, commission_rate, status, created_at, updated_at
	         FROM users`

	if role == models.RoleSuperAdmin {
		if filterRole != "" {
			rows, err = h.db.QueryContext(r.Context(),
				base+` WHERE role = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
				filterRole, limit, offset)
		} else {
			rows, err = h.db.QueryContext(r.Context(),
				base+` ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
				limit, offset)
		}
	} else {
		if filterRole != "" {
			rows, err = h.db.QueryContext(r.Context(),
				base+` WHERE path <@ (SELECT path FROM users WHERE id = $1)
				       AND id != $1 AND role = $2
				       ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
				userID, filterRole, limit, offset)
		} else {
			rows, err = h.db.QueryContext(r.Context(),
				base+` WHERE path <@ (SELECT path FROM users WHERE id = $1)
				       AND id != $1
				       ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
				userID, limit, offset)
		}
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	users := []*models.User{}
	for rows.Next() {
		u := &models.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
			&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
			&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}
	httputil.WriteJSON(w, http.StatusOK, users)
}

// PanelAudit returns audit-log rows for the caller's downline (plus the
// caller's own entries). SuperAdmin gets the full audit stream.
func (h *Handler) PanelAudit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	role := roleFromCtx(r)
	if !panelRoleAllowed(role) {
		httputil.WriteError(w, http.StatusForbidden, "clients cannot access audit log")
		return
	}

	limit := parseIntParam(r, "limit", 200)
	action := r.URL.Query().Get("action")

	var (
		rows *sql.Rows
		err  error
	)

	if role == models.RoleSuperAdmin {
		if action != "" {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT id, actor_id, action, entity_type, entity_id, ip_address::text, created_at
				 FROM audit_log
				 WHERE action = $1
				 ORDER BY created_at DESC LIMIT $2`,
				action, limit)
		} else {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT id, actor_id, action, entity_type, entity_id, ip_address::text, created_at
				 FROM audit_log
				 ORDER BY created_at DESC LIMIT $1`, limit)
		}
	} else {
		// Restrict to actors within the caller's ltree subtree.
		if action != "" {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT a.id, a.actor_id, a.action, a.entity_type, a.entity_id,
				        a.ip_address::text, a.created_at
				 FROM audit_log a
				 JOIN users u ON u.id = a.actor_id
				 WHERE u.path <@ (SELECT path FROM users WHERE id = $1)
				   AND a.action = $2
				 ORDER BY a.created_at DESC LIMIT $3`,
				userID, action, limit)
		} else {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT a.id, a.actor_id, a.action, a.entity_type, a.entity_id,
				        a.ip_address::text, a.created_at
				 FROM audit_log a
				 JOIN users u ON u.id = a.actor_id
				 WHERE u.path <@ (SELECT path FROM users WHERE id = $1)
				 ORDER BY a.created_at DESC LIMIT $2`,
				userID, limit)
		}
	}
	if err != nil {
		// If the audit_log table is missing (e.g. in a dev DB without phase2
		// migrations) return an empty list rather than 500 so the panel UI
		// keeps working.
		h.logger.WarnContext(r.Context(), "panel audit query failed", "error", err)
		httputil.WriteJSON(w, http.StatusOK, []interface{}{})
		return
	}
	defer rows.Close()

	entries := []map[string]interface{}{}
	for rows.Next() {
		var id, actorID int64
		var action, entityType, entityID string
		var ipAddr sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &actorID, &action, &entityType, &entityID, &ipAddr, &createdAt); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entry := map[string]interface{}{
			"id":          id,
			"actor_id":    actorID,
			"action":      action,
			"entity_type": entityType,
			"entity_id":   entityID,
			"created_at":  createdAt,
		}
		if ipAddr.Valid {
			entry["ip_address"] = ipAddr.String
		}
		entries = append(entries, entry)
	}
	httputil.WriteJSON(w, http.StatusOK, entries)
}

// PanelPnL returns a daily P&L roll-up across the caller's downline.
func (h *Handler) PanelPnL(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	role := roleFromCtx(r)
	if !panelRoleAllowed(role) {
		httputil.WriteError(w, http.StatusForbidden, "clients cannot access panel")
		return
	}

	from, to := parseDateRange(r)

	var (
		rows *sql.Rows
		err  error
	)

	if role == models.RoleSuperAdmin {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT to_char(date_trunc('day', b.created_at), 'YYYY-MM-DD') AS day,
			        COUNT(*), COALESCE(SUM(b.stake), 0),
			        COALESCE(SUM(CASE WHEN b.status = 'settled' THEN b.profit ELSE 0 END), 0)
			 FROM bets b
			 WHERE b.created_at BETWEEN $1 AND $2
			 GROUP BY day ORDER BY day`,
			from, to)
	} else {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT to_char(date_trunc('day', b.created_at), 'YYYY-MM-DD') AS day,
			        COUNT(*), COALESCE(SUM(b.stake), 0),
			        COALESCE(SUM(CASE WHEN b.status = 'settled' THEN b.profit ELSE 0 END), 0)
			 FROM bets b
			 JOIN users u ON u.id = b.user_id
			 WHERE u.path <@ (SELECT path FROM users WHERE id = $1)
			   AND b.created_at BETWEEN $2 AND $3
			 GROUP BY day ORDER BY day`,
			userID, from, to)
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	result := []map[string]interface{}{}
	for rows.Next() {
		var day string
		var bets int
		var stake, pnl float64
		if err := rows.Scan(&day, &bets, &stake, &pnl); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result = append(result, map[string]interface{}{
			"date":  day,
			"bets":  bets,
			"stake": stake,
			"pnl":   pnl,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

// PanelVolume returns bet volume grouped by sport across the caller's
// downline. Useful for quick "where are we taking action" summaries.
func (h *Handler) PanelVolume(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	role := roleFromCtx(r)
	if !panelRoleAllowed(role) {
		httputil.WriteError(w, http.StatusForbidden, "clients cannot access panel")
		return
	}

	from, to := parseDateRange(r)

	var (
		rows *sql.Rows
		err  error
	)

	if role == models.RoleSuperAdmin {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT COALESCE(m.sport, 'unknown') AS sport,
			        COUNT(b.id), COALESCE(SUM(b.stake), 0)
			 FROM bets b
			 LEFT JOIN markets m ON m.id = b.market_id
			 WHERE b.created_at BETWEEN $1 AND $2
			 GROUP BY sport ORDER BY SUM(b.stake) DESC NULLS LAST`,
			from, to)
	} else {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT COALESCE(m.sport, 'unknown') AS sport,
			        COUNT(b.id), COALESCE(SUM(b.stake), 0)
			 FROM bets b
			 JOIN users u ON u.id = b.user_id
			 LEFT JOIN markets m ON m.id = b.market_id
			 WHERE u.path <@ (SELECT path FROM users WHERE id = $1)
			   AND b.created_at BETWEEN $2 AND $3
			 GROUP BY sport ORDER BY SUM(b.stake) DESC NULLS LAST`,
			userID, from, to)
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	result := []map[string]interface{}{}
	for rows.Next() {
		var sport string
		var bets int
		var volume float64
		if err := rows.Scan(&sport, &bets, &volume); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result = append(result, map[string]interface{}{
			"sport":  sport,
			"bets":   bets,
			"volume": volume,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

// panelRoleAllowed reports whether the caller role may access panel routes.
// Clients are explicitly excluded; every other role (agent through
// superadmin) is allowed and results are scoped to their downline.
func panelRoleAllowed(role models.Role) bool {
	switch role {
	case models.RoleAgent, models.RoleMaster, models.RoleAdmin, models.RoleSuperAdmin:
		return true
	}
	return false
}

func roleFromCtx(r *http.Request) models.Role {
	return middleware.RoleFromContext(r.Context())
}

func (h *Handler) FraudAlerts(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r, "limit", 50)
	var resolved *bool
	if rv := r.URL.Query().Get("resolved"); rv != "" {
		b := rv == "true"
		resolved = &b
	}

	alerts, err := h.fraud.GetAlerts(r.Context(), resolved, limit)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "fraud alerts query failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load fraud alerts")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, alerts)
}

// Helpers

func parseDateRange(r *http.Request) (time.Time, time.Time) {
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			to = parsed.Add(24*time.Hour - time.Second)
		}
	}
	return from, to
}

func parseIntParam(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			if i > 500 {
				return 500
			}
			return i
		}
	}
	return def
}

func parsePathInt64(r *http.Request, key string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid path parameter %s", key)
	}
	return v, nil
}
