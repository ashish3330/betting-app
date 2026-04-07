package casino

import (
	"encoding/json"
	"net/http"

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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	token := r.URL.Query().Get("token")

	session, err := h.service.ValidateSession(r.Context(), sessionID, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) CloseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if err := h.service.CloseSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "session closed"})
}

func (h *Handler) SettlementWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string  `json:"session_id"`
		RoundID   string  `json:"round_id"`
		Stake     float64 `json:"stake"`
		Payout    float64 `json:"payout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.HandleSettlementWebhook(r.Context(), req.SessionID, req.RoundID, req.Stake, req.Payout); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "settled"})
}

func (h *Handler) SessionHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	sessions, err := h.service.GetSessionHistory(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
