package hierarchy

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	mux.HandleFunc("GET /api/v1/hierarchy/children", h.GetChildren)
	mux.HandleFunc("GET /api/v1/hierarchy/children/direct", h.GetDirectChildren)
	mux.HandleFunc("POST /api/v1/hierarchy/credit/transfer", h.TransferCredit)
	mux.HandleFunc("GET /api/v1/hierarchy/user/{id}", h.GetUser)
	mux.HandleFunc("PUT /api/v1/hierarchy/user/{id}/status", h.UpdateStatus)
}

func (h *Handler) GetChildren(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	children, err := h.service.GetChildren(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load children")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, children)
}

func (h *Handler) GetDirectChildren(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	children, err := h.service.GetDirectChildren(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to load direct children")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, children)
}

func (h *Handler) TransferCredit(w http.ResponseWriter, r *http.Request) {
	var req models.CreditTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	callerID := middleware.UserIDFromContext(r.Context())
	if req.FromUserID != callerID {
		httputil.WriteError(w, http.StatusForbidden, "can only transfer from your own account")
		return
	}

	if err := h.service.TransferCredit(r.Context(), &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "credit transfer failed")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "credit transferred"})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	callerID := middleware.UserIDFromContext(r.Context())
	isAncestor, err := h.service.IsAncestor(r.Context(), callerID, id)
	if err != nil || !isAncestor {
		httputil.WriteError(w, http.StatusForbidden, "no access to this user")
		return
	}

	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	callerID := middleware.UserIDFromContext(r.Context())
	isAncestor, err := h.service.IsAncestor(r.Context(), callerID, id)
	if err != nil || !isAncestor {
		httputil.WriteError(w, http.StatusForbidden, "no access to this user")
		return
	}

	// Prevent reactivation of self-excluded users during exclusion period
	if req.Status == "active" {
		excluded, until, checkErr := h.service.IsUserSelfExcluded(r.Context(), id)
		if checkErr != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to check self-exclusion status")
			return
		}
		if excluded {
			httputil.WriteError(w, http.StatusForbidden, "user is self-excluded until "+until.Format("2006-01-02")+"; cannot reactivate during exclusion period")
			return
		}
	}

	if err := h.service.UpdateUserStatus(r.Context(), id, req.Status); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "status updated"})
}
