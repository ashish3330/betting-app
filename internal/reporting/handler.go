package reporting

import (
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
	mux.HandleFunc("GET /api/v1/reports/pnl", h.GetPnL)
	mux.HandleFunc("GET /api/v1/reports/market/{id}", h.GetMarketReport)
	mux.HandleFunc("GET /api/v1/reports/dashboard", h.GetDashboard)
	mux.HandleFunc("GET /api/v1/reports/volume", h.GetBetVolume)
	mux.HandleFunc("GET /api/v1/reports/hierarchy/pnl", h.GetHierarchyPnL)
}

// requireAdmin checks that the caller has an admin or superadmin role.
func requireAdmin(r *http.Request) bool {
	role := middleware.RoleFromContext(r.Context())
	return role == models.Role("superadmin") || role == models.Role("admin")
}

func (h *Handler) GetPnL(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	from, to := parseDateRange(r)

	report, err := h.service.GetUserPnL(r.Context(), userID, from, to)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to generate PnL report")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) GetMarketReport(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(r) {
		httputil.WriteError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	marketID := r.PathValue("id")
	report, err := h.service.GetMarketReport(r.Context(), marketID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "market report not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(r) {
		httputil.WriteError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	stats, err := h.service.GetDashboardStats(r.Context())
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load dashboard stats")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, stats)
}

func (h *Handler) GetBetVolume(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(r) {
		httputil.WriteError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	from, to := parseDateRange(r)

	interval := 15
	if i := r.URL.Query().Get("interval"); i != "" {
		if v, err := strconv.Atoi(i); err == nil && v > 0 && v <= 1440 {
			interval = v
		}
	}

	points, err := h.service.GetBetVolumeTrend(r.Context(), from, to, interval)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load bet volume data")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, points)
}

func (h *Handler) GetHierarchyPnL(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	from, to := parseDateRange(r)

	reports, err := h.service.GetHierarchyPnL(r.Context(), userID, from, to)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to generate hierarchy PnL report")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, reports)
}

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
