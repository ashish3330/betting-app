package matching

import (
	"encoding/json"
	"net/http"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
)

type Handler struct {
	engine  *Engine
	wallet  *wallet.Service
}

func NewHandler(engine *Engine, walletSvc *wallet.Service) *Handler {
	return &Handler{engine: engine, wallet: walletSvc}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/bet/place", h.PlaceBet)
	mux.HandleFunc("DELETE /api/v1/bet/{betId}/cancel", h.CancelBet)
	mux.HandleFunc("GET /api/v1/market/{marketId}/orderbook", h.GetOrderBook)
}

func (h *Handler) PlaceBet(w http.ResponseWriter, r *http.Request) {
	var req models.PlaceBetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID := middleware.UserIDFromContext(r.Context())

	// Calculate liability and hold funds
	var holdAmount float64
	if req.Side == models.BetSideBack {
		holdAmount = req.Stake
	} else {
		holdAmount = req.Stake * (req.Price - 1) // lay liability
	}

	// Place and match atomically
	result, err := h.engine.PlaceAndMatch(r.Context(), &req, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Hold funds for unmatched portion
	if result.UnmatchedStake > 0 {
		unmatchedHold := holdAmount * (result.UnmatchedStake / req.Stake)
		if err := h.wallet.HoldFunds(r.Context(), userID, unmatchedHold, result.BetID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CancelBet(w http.ResponseWriter, r *http.Request) {
	betID := r.PathValue("betId")
	marketID := r.URL.Query().Get("market_id")
	side := models.BetSide(r.URL.Query().Get("side"))

	if marketID == "" || (side != models.BetSideBack && side != models.BetSideLay) {
		writeError(w, http.StatusBadRequest, "market_id and valid side required")
		return
	}

	if err := h.engine.CancelOrder(r.Context(), marketID, betID, side); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "order cancelled", "bet_id": betID})
}

func (h *Handler) GetOrderBook(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("marketId")

	backs, lays, err := h.engine.GetOrderBook(r.Context(), marketID, 5)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"market_id": marketID,
		"back":      backs,
		"lay":       lays,
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
