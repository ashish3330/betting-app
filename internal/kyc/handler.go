package kyc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	// User routes
	mux.HandleFunc("GET /api/v1/kyc/status", h.GetStatus)
	mux.HandleFunc("POST /api/v1/kyc/submit", h.Submit)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	// Admin routes
	mux.HandleFunc("GET /api/v1/admin/kyc/pending", h.PendingList)
	mux.HandleFunc("POST /api/v1/admin/kyc/{id}/approve", h.Approve)
	mux.HandleFunc("POST /api/v1/admin/kyc/{id}/reject", h.Reject)
}

func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	profile, err := h.service.GetKYCStatus(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve KYC status")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req SubmitKYCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	profile, err := h.service.SubmitKYC(r.Context(), userID, &req)
	if err != nil {
		// Validation errors are safe to return; internal errors are not
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "KYC already") {
			httputil.WriteError(w, http.StatusBadRequest, errMsg)
		} else {
			httputil.WriteError(w, http.StatusBadRequest, "failed to submit KYC")
		}
		return
	}
	httputil.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) PendingList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	profiles, err := h.service.GetPendingKYC(r.Context(), limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve pending KYC list")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, profiles)
}

func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	adminID := middleware.UserIDFromContext(r.Context())
	if err := h.service.ApproveKYC(r.Context(), userID, adminID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to approve KYC")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "KYC approved"})
}

func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	var req struct {
		Reason string `json:"reason" validate:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	adminID := middleware.UserIDFromContext(r.Context())
	if err := h.service.RejectKYC(r.Context(), userID, adminID, req.Reason); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to reject KYC")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "KYC rejected"})
}
