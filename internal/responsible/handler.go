package responsible

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
	mux.HandleFunc("GET /api/v1/responsible-gambling/limits", h.GetLimits)
	mux.HandleFunc("PUT /api/v1/responsible-gambling/limits", h.UpdateLimits)
	mux.HandleFunc("POST /api/v1/responsible-gambling/self-exclude", h.SelfExclude)
	mux.HandleFunc("POST /api/v1/responsible-gambling/cooling-off", h.CoolingOff)
	mux.HandleFunc("GET /api/v1/responsible-gambling/session", h.SessionInfo)
}

func (h *Handler) GetLimits(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	limits, err := h.service.GetLimits(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (h *Handler) UpdateLimits(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req UpdateLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	limits, err := h.service.UpdateLimits(r.Context(), userID, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (h *Handler) SelfExclude(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req SelfExclusionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.SelfExclude(r.Context(), userID, &req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "self-exclusion activated"})
}

func (h *Handler) CoolingOff(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req struct {
		Hours int `json:"hours" validate:"required,gte=1,lte=72"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Hours < 1 || req.Hours > 72 {
		writeError(w, http.StatusBadRequest, "hours must be between 1 and 72")
		return
	}

	if err := h.service.SetCoolingOff(r.Context(), userID, req.Hours); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "cooling-off period activated"})
}

func (h *Handler) SessionInfo(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	duration, limit, err := h.service.GetSessionDuration(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_duration_mins": int(duration.Minutes()),
		"session_limit_mins":   limit,
		"remaining_mins":       limit - int(duration.Minutes()),
		"should_warn":          int(duration.Minutes()) >= limit-10,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
