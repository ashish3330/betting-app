package matching

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/lotus-exchange/lotus-exchange/pkg/httputil"
)

type Handler struct {
	engine *Engine
	wallet *wallet.Service
	db     *sql.DB
	logger *slog.Logger
}

func NewHandler(engine *Engine, walletSvc *wallet.Service, db *sql.DB, logger *slog.Logger) *Handler {
	return &Handler{engine: engine, wallet: walletSvc, db: db, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/bet/place", h.PlaceBet)
	mux.HandleFunc("DELETE /api/v1/bet/{betId}/cancel", h.CancelBet)
	mux.HandleFunc("GET /api/v1/market/{marketId}/orderbook", h.GetOrderBook)
	mux.HandleFunc("GET /api/v1/bets", h.UserBets)
	mux.HandleFunc("GET /api/v1/bets/history", h.BetsHistoryHandler)
	mux.HandleFunc("GET /api/v1/positions/{marketId}", h.GetPositions)
}

func (h *Handler) PlaceBet(w http.ResponseWriter, r *http.Request) {
	// Issue #6: Request body size limit
	r.Body = http.MaxBytesReader(w, r.Body, 65536)

	var req models.PlaceBetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID := middleware.UserIDFromContext(r.Context())

	// Issue #1: Calculate full liability BEFORE matching and hold funds first
	var holdAmount float64
	if req.Side == models.BetSideBack {
		holdAmount = req.Stake
	} else {
		holdAmount = req.Stake * (req.Price - 1) // lay liability
	}

	// Hold the full amount before placing the order
	// Use a temporary bet ID for the hold; we'll get the real one from PlaceAndMatch
	if err := h.wallet.HoldFunds(r.Context(), userID, holdAmount, fmt.Sprintf("pre:%s:%s", req.MarketID, req.ClientRef)); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Place and match atomically
	result, err := h.engine.PlaceAndMatch(r.Context(), &req, userID)
	if err != nil {
		// Issue #5: Rollback the fund hold if PlaceAndMatch fails
		if releaseErr := h.wallet.ReleaseFunds(r.Context(), userID, holdAmount, fmt.Sprintf("pre:%s:%s", req.MarketID, req.ClientRef)); releaseErr != nil {
			h.logger.ErrorContext(r.Context(), "failed to release funds after PlaceAndMatch failure",
				"user_id", userID, "amount", holdAmount, "error", releaseErr)
		}
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Release the hold for the matched portion (settlement handles matched funds separately)
	if result.MatchedStake > 0 {
		matchedHold := holdAmount * (result.MatchedStake / req.Stake)
		if releaseErr := h.wallet.ReleaseFunds(r.Context(), userID, matchedHold, fmt.Sprintf("pre:%s:%s", req.MarketID, req.ClientRef)); releaseErr != nil {
			h.logger.ErrorContext(r.Context(), "failed to release matched portion hold",
				"user_id", userID, "amount", matchedHold, "bet_id", result.BetID, "error", releaseErr)
		}
	}

	// Issue #4: Persist order to PostgreSQL so it survives Redis restarts
	if h.db != nil {
		if persistErr := h.engine.PersistOrder(r.Context(), h.db, &req, userID, result); persistErr != nil {
			h.logger.ErrorContext(r.Context(), "failed to persist order to database",
				"bet_id", result.BetID, "error", persistErr)
			// Non-fatal: the order is already in Redis and funds are held.
			// A recovery process can reconcile later.
		}
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) CancelBet(w http.ResponseWriter, r *http.Request) {
	betID := r.PathValue("betId")
	marketID := r.URL.Query().Get("market_id")
	side := models.BetSide(r.URL.Query().Get("side"))

	if marketID == "" || (side != models.BetSideBack && side != models.BetSideLay) {
		httputil.WriteError(w, http.StatusBadRequest, "market_id and valid side required")
		return
	}

	// Ownership is now verified atomically inside the Lua cancel script,
	// so there is no window where the order is removed before we check ownership.
	userID := middleware.UserIDFromContext(r.Context())

	cancelled, err := h.engine.CancelOrder(r.Context(), marketID, betID, side, userID)
	if err != nil {
		if strings.Contains(err.Error(), "belongs to another user") {
			httputil.WriteError(w, http.StatusForbidden, "you do not own this order")
			return
		}
		httputil.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	// Release held funds for the unmatched (cancelled) portion
	var releaseAmount float64
	if cancelled.Side == models.BetSideBack {
		releaseAmount = cancelled.Remaining
	} else {
		releaseAmount = cancelled.Remaining * (cancelled.Price - 1) // lay liability
	}
	if releaseAmount > 0 {
		if releaseErr := h.wallet.ReleaseFunds(r.Context(), userID, releaseAmount, betID); releaseErr != nil {
			h.logger.ErrorContext(r.Context(), "failed to release funds on cancel",
				"user_id", userID, "amount", releaseAmount, "bet_id", betID, "error", releaseErr)
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "order cancelled", "bet_id": betID})
}

func (h *Handler) GetOrderBook(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("marketId")

	backs, lays, err := h.engine.GetOrderBook(r.Context(), marketID, 5)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"market_id": marketID,
		"back":      backs,
		"lay":       lays,
	})
}

// UserBets implements GET /api/v1/bets — the bet listing endpoint used by
// the navbar exposure popup, the bets page, and the account history page.
// Supports ?status=open|matched|settled|cancelled|void, ?market_id=<id>,
// ?page=<n> and ?limit=<n> query parameters.
func (h *Handler) UserBets(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		httputil.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	q := r.URL.Query()
	statusFilter := q.Get("status")
	marketFilter := q.Get("market_id")

	// Both page/limit and limit/offset are accepted: the integration test
	// and some frontend callers use limit/offset while others use page/limit.
	// We translate offset into an equivalent page number so the service
	// layer stays page-based.
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if offsetStr := q.Get("offset"); offsetStr != "" && page == 0 {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			effLimit := limit
			if effLimit <= 0 || effLimit > 100 {
				effLimit = 50
			}
			page = offset/effLimit + 1
		}
	}

	result, err := h.ListUserBets(r.Context(), userID, statusFilter, marketFilter, page, limit)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list user bets failed", "user_id", userID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to fetch bets")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// BetsHistoryHandler implements GET /api/v1/bets/history — returns every
// valid-status bet for the authenticated user plus aggregate summary stats
// (total stake, PnL, won/lost/pending counts).
//
// Replaces the previous inline handler in cmd/matching-engine/main.go which
// incorrectly queried the non-existent betting.orders table. See the skip
// list in scripts/api-test/main.go for the bug report.
func (h *Handler) BetsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		httputil.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	result, err := h.BetsHistory(r.Context(), userID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "bet history failed", "user_id", userID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to fetch bet history")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// GetPositions implements GET /api/v1/positions/{marketId} — returns the
// user's net matched position per runner for a single market.
func (h *Handler) GetPositions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		httputil.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	marketID := r.PathValue("marketId")
	if marketID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "market_id required")
		return
	}

	result, err := h.GetUserPositionsForMarket(r.Context(), userID, marketID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "positions query failed", "user_id", userID, "market_id", marketID, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to fetch positions")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}
