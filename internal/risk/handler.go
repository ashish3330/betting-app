package risk

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/risk/market/{id}", h.MarketExposure)
	mux.HandleFunc("GET /api/v1/risk/user/{id}", h.UserExposure)
	mux.HandleFunc("GET /api/v1/risk/exposure", h.MyExposure)
}

func isAdminRole(role models.Role) bool {
	return role == models.RoleSuperAdmin || role == models.RoleAdmin
}

func (h *Handler) MarketExposure(w http.ResponseWriter, r *http.Request) {
	role := middleware.RoleFromContext(r.Context())
	if !isAdminRole(role) {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	marketID := r.PathValue("id")
	exposure, err := h.service.GetMarketExposure(r.Context(), marketID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve market exposure")
		return
	}
	writeJSON(w, http.StatusOK, exposure)
}

func (h *Handler) UserExposure(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	// Allow admin/superadmin to view any user, or the user to view their own data
	requestingUserID := middleware.UserIDFromContext(r.Context())
	role := middleware.RoleFromContext(r.Context())
	if !isAdminRole(role) && requestingUserID != userID {
		writeError(w, http.StatusForbidden, "not authorized to view this user's exposure")
		return
	}

	exposure, err := h.service.GetUserExposure(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve user exposure")
		return
	}
	writeJSON(w, http.StatusOK, exposure)
}

func (h *Handler) MyExposure(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	exposure, err := h.service.GetUserExposure(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve exposure")
		return
	}
	writeJSON(w, http.StatusOK, exposure)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
