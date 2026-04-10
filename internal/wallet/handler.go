package wallet

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/wallet/balance", h.GetBalance)
	mux.HandleFunc("GET /api/v1/wallet/ledger", h.GetLedger)
	mux.HandleFunc("POST /api/v1/wallet/deposit", h.Deposit)
}

func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	summary, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		slog.Error("get balance failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve balance")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, summary)
}

func (h *Handler) GetStatement(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	from := time.Now().AddDate(0, -1, 0) // default: last 30 days
	to := time.Now()
	if f := r.URL.Query().Get("from"); f != "" {
		if parsed, err := time.Parse(time.RFC3339, f); err == nil {
			from = parsed
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}

	entries, err := h.service.GetStatements(r.Context(), userID, from, to, limit, offset)
	if err != nil {
		slog.Error("get statement failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve statement")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, entries)
}

func (h *Handler) GetLedger(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	entries, err := h.service.GetLedger(r.Context(), userID, limit, offset)
	if err != nil {
		slog.Error("get ledger failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve ledger")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, entries)
}

func (h *Handler) Deposit(w http.ResponseWriter, r *http.Request) {
	// Only admin or superadmin may call the direct deposit endpoint.
	role := middleware.RoleFromContext(r.Context())
	if role != models.RoleSuperAdmin && role != models.RoleAdmin {
		httputil.WriteError(w, http.StatusForbidden, "not authorized")
		return
	}

	var req struct {
		UserID    int64   `json:"user_id"`
		Amount    float64 `json:"amount"`
		Reference string  `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Amount <= 0 {
		httputil.WriteError(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	if req.UserID <= 0 {
		httputil.WriteError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	if err := h.service.Deposit(r.Context(), req.UserID, req.Amount, req.Reference); err != nil {
		slog.Error("deposit failed", "error", err, "user_id", req.UserID)
		httputil.WriteError(w, http.StatusInternalServerError, "deposit failed")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "deposit successful"})
}
