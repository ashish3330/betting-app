package fraud

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/fraud/alerts", h.GetAlerts)
	mux.HandleFunc("POST /api/v1/fraud/alerts/{id}/resolve", h.ResolveAlert)
	mux.HandleFunc("GET /api/v1/fraud/user/{id}/risk", h.GetUserRisk)
}

func (h *Handler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	var resolved *bool
	if rv := r.URL.Query().Get("resolved"); rv != "" {
		b := rv == "true"
		resolved = &b
	}

	alerts, err := h.service.GetAlerts(r.Context(), resolved, limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve fraud alerts")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, alerts)
}

func (h *Handler) ResolveAlert(w http.ResponseWriter, r *http.Request) {
	alertID := r.PathValue("id")
	adminID := middleware.UserIDFromContext(r.Context())

	var req struct {
		Resolution string `json:"resolution"`
	}
	// Decode optional resolution note; ignore decode errors (body may be empty)
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.service.ResolveAlert(r.Context(), alertID, adminID, req.Resolution); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to resolve alert")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "alert resolved"})
}

func (h *Handler) GetUserRisk(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	score, level, err := h.service.GetUserRiskScore(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve risk score")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":    userID,
		"risk_score": score,
		"risk_level": level,
	})
}
