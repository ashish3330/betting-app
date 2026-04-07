package reporting

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
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

func (h *Handler) GetPnL(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	from, to := parseDateRange(r)

	report, err := h.service.GetUserPnL(r.Context(), userID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) GetMarketReport(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	report, err := h.service.GetMarketReport(r.Context(), marketID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.GetDashboardStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) GetBetVolume(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)

	interval := 15
	if i := r.URL.Query().Get("interval"); i != "" {
		if v, err := strconv.Atoi(i); err == nil && v > 0 && v <= 1440 {
			interval = v
		}
	}

	points, err := h.service.GetBetVolumeTrend(r.Context(), from, to, interval)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (h *Handler) GetHierarchyPnL(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	from, to := parseDateRange(r)

	reports, err := h.service.GetHierarchyPnL(r.Context(), userID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reports)
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
