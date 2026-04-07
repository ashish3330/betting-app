package admin

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/fraud"
	"github.com/lotus-exchange/lotus-exchange/internal/market"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/reporting"
	"github.com/lotus-exchange/lotus-exchange/internal/settlement"
)

type Handler struct {
	db         *sql.DB
	market     *market.Service
	settlement *settlement.Service
	reporting  *reporting.Service
	fraud      *fraud.Service
	logger     *slog.Logger
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
			&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
			&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}

	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID")
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
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	id := parsePathInt64(r, "id")
	var req struct {
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	_, err := h.db.ExecContext(r.Context(),
		"UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2",
		req.Status, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.InfoContext(r.Context(), "admin updated user status",
		"admin", middleware.UserIDFromContext(r.Context()), "user", id, "status", req.Status)

	writeJSON(w, http.StatusOK, map[string]string{"message": "status updated"})
}

func (h *Handler) UpdateCreditLimit(w http.ResponseWriter, r *http.Request) {
	id := parsePathInt64(r, "id")
	var req struct {
		CreditLimit float64 `json:"credit_limit"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	_, err := h.db.ExecContext(r.Context(),
		"UPDATE users SET credit_limit = $1, updated_at = NOW() WHERE id = $2",
		req.CreditLimit, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "credit limit updated"})
}

func (h *Handler) UpdateCommission(w http.ResponseWriter, r *http.Request) {
	id := parsePathInt64(r, "id")
	var req struct {
		CommissionRate float64 `json:"commission_rate"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	_, err := h.db.ExecContext(r.Context(),
		"UPDATE users SET commission_rate = $1, updated_at = NOW() WHERE id = $2",
		req.CommissionRate, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "commission rate updated"})
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, markets)
}

func (h *Handler) SuspendMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.market.UpdateStatus(r.Context(), marketID, models.MarketSuspended); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.InfoContext(r.Context(), "market suspended",
		"admin", middleware.UserIDFromContext(r.Context()), "market", marketID)

	writeJSON(w, http.StatusOK, map[string]string{"message": "market suspended"})
}

func (h *Handler) ResumeMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.market.UpdateStatus(r.Context(), marketID, models.MarketOpen); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "market resumed"})
}

func (h *Handler) SettleMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	var req struct {
		WinnerSelectionID int64 `json:"winner_selection_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	result, err := h.settlement.SettleMarket(r.Context(), marketID, req.WinnerSelectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.InfoContext(r.Context(), "market settled by admin",
		"admin", middleware.UserIDFromContext(r.Context()), "market", marketID, "winner", req.WinnerSelectionID)

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) VoidMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if err := h.settlement.VoidMarket(r.Context(), marketID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "market voided"})
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var bets []*models.Bet
	for rows.Next() {
		b := &models.Bet{}
		if err := rows.Scan(&b.ID, &b.MarketID, &b.SelectionID, &b.UserID, &b.Side,
			&b.Price, &b.Stake, &b.MatchedStake, &b.UnmatchedStake, &b.Profit,
			&b.Status, &b.ClientRef, &b.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		bets = append(bets, b)
	}

	writeJSON(w, http.StatusOK, bets)
}

func (h *Handler) PnLReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	userIDStr := r.URL.Query().Get("user_id")

	if userIDStr != "" {
		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		report, err := h.reporting.GetUserPnL(r.Context(), userID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
		return
	}

	// Hierarchy P&L for the admin
	adminID := middleware.UserIDFromContext(r.Context())
	reports, err := h.reporting.GetHierarchyPnL(r.Context(), adminID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (h *Handler) VolumeReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	interval := parseIntParam(r, "interval", 15)

	points, err := h.reporting.GetBetVolumeTrend(r.Context(), from, to, interval)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, points)
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, alerts)
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
			return i
		}
	}
	return def
}

func parsePathInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	return v
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
