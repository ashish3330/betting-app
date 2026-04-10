package market

import (
	"encoding/json"
	"net/http"

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
	mux.HandleFunc("GET /api/v1/markets/list", h.List)
	mux.HandleFunc("GET /api/v1/markets/detail/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/markets/create", h.Create)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sport := r.URL.Query().Get("sport")
	status := r.URL.Query().Get("status")
	var inPlay *bool
	if ip := r.URL.Query().Get("in_play"); ip != "" {
		b := ip == "true"
		inPlay = &b
	}

	markets, err := h.service.List(r.Context(), sport, status, inPlay)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list markets")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, markets)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	m, err := h.service.Get(r.Context(), marketID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "market not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	role := middleware.RoleFromContext(r.Context())
	if role != models.Role("superadmin") && role != models.Role("admin") {
		httputil.WriteError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var m models.Market
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.Create(r.Context(), &m); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to create market")
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, m)
}
