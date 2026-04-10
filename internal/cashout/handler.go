package cashout

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/cashout/offer", h.GetOffer)
	mux.HandleFunc("POST /api/v1/cashout/accept", h.AcceptOffer)
	mux.HandleFunc("GET /api/v1/cashout/offers", h.ListOffers)
}

func (h *Handler) GetOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req struct {
		BetID string `json:"bet_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BetID == "" {
		writeError(w, http.StatusBadRequest, "bet_id is required")
		return
	}

	offer, err := h.service.GenerateOffer(r.Context(), req.BetID, userID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "not eligible") ||
			strings.Contains(errMsg, "no matched") || strings.Contains(errMsg, "not available") {
			writeError(w, http.StatusBadRequest, errMsg)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to generate cashout offer")
		}
		return
	}
	writeJSON(w, http.StatusOK, offer)
}

func (h *Handler) AcceptOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req struct {
		OfferID string `json:"offer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OfferID == "" {
		writeError(w, http.StatusBadRequest, "offer_id is required")
		return
	}

	offer, err := h.service.AcceptOffer(r.Context(), req.OfferID, userID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "expired") ||
			strings.Contains(errMsg, "already") || strings.Contains(errMsg, "contact support") {
			writeError(w, http.StatusBadRequest, errMsg)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to accept cashout offer")
		}
		return
	}
	writeJSON(w, http.StatusOK, offer)
}

func (h *Handler) ListOffers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	offers, err := h.service.GetUserOffers(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve cashout offers")
		return
	}
	writeJSON(w, http.StatusOK, offers)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
