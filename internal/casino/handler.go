package casino

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/casino/providers", h.ListProviders)
	mux.HandleFunc("GET /api/v1/casino/games", h.ListGames)
	mux.HandleFunc("POST /api/v1/casino/session", h.CreateSession)
	mux.HandleFunc("GET /api/v1/casino/session/{id}", h.GetSession)
	mux.HandleFunc("DELETE /api/v1/casino/session/{id}", h.CloseSession)
	mux.HandleFunc("POST /api/v1/casino/webhook/settlement", h.SettlementWebhook)
	mux.HandleFunc("GET /api/v1/casino/history", h.SessionHistory)
}

func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers := h.service.ListProviders()
	writeJSON(w, http.StatusOK, providers)
}

func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	games := h.service.ListGames()
	writeJSON(w, http.StatusOK, games)
}

func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	// TODO: implement category listing
	writeJSON(w, http.StatusOK, []string{})
}

func (h *Handler) ListGamesByCategory(w http.ResponseWriter, r *http.Request) {
	// TODO: implement games by category listing
	writeJSON(w, http.StatusOK, []string{})
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GameType   GameType `json:"game_type"`
		ProviderID string   `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	session, err := h.service.CreateSession(r.Context(), userID, req.GameType, req.ProviderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create session")
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	token := r.URL.Query().Get("token")

	session, err := h.service.ValidateSession(r.Context(), sessionID, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) CloseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	userID := middleware.UserIDFromContext(r.Context())

	// Verify the authenticated user owns the session before closing
	owner, err := h.service.GetSessionOwner(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if owner != userID {
		writeError(w, http.StatusForbidden, "not authorized to close this session")
		return
	}

	if err := h.service.CloseSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "session closed"})
}

func (h *Handler) SettlementWebhook(w http.ResponseWriter, r *http.Request) {
	// HMAC-SHA256 signature verification
	webhookSecret := os.Getenv("CASINO_WEBHOOK_SECRET")
	if webhookSecret == "" {
		writeError(w, http.StatusInternalServerError, "webhook not configured")
		return
	}

	signature := r.Header.Get("X-Webhook-Signature")
	if signature == "" {
		writeError(w, http.StatusUnauthorized, "missing webhook signature")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	// After signature verification succeeds, always return 200 to prevent
	// retry storms from the casino provider. Log errors server-side.

	var req struct {
		SessionID string  `json:"session_id"`
		RoundID   string  `json:"round_id"`
		Stake     float64 `json:"stake"`
		Payout    float64 `json:"payout"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.service.logger.Error("casino webhook: invalid JSON body", "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Validate amounts: reject negative values and unreasonable payouts
	if req.Stake < 0 || req.Payout < 0 {
		h.service.logger.Error("casino webhook: negative stake or payout",
			"stake", req.Stake, "payout", req.Payout)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if req.Stake > 0 && req.Payout > req.Stake*10000 {
		h.service.logger.Error("casino webhook: payout exceeds max multiplier",
			"stake", req.Stake, "payout", req.Payout)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if err := h.service.HandleSettlementWebhook(r.Context(), req.SessionID, req.RoundID, req.Stake, req.Payout); err != nil {
		h.service.logger.Error("casino settlement processing failed", "error", err,
			"session_id", req.SessionID, "round_id", req.RoundID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "settled"})
}

func (h *Handler) SessionHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	sessions, err := h.service.GetSessionHistory(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve session history")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
