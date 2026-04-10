package payment

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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
	mux.HandleFunc("POST /api/v1/payment/deposit/upi", h.UPIDeposit)
	mux.HandleFunc("POST /api/v1/payment/deposit/crypto", h.CryptoDeposit)
	mux.HandleFunc("POST /api/v1/payment/withdraw", h.Withdraw)
	mux.HandleFunc("GET /api/v1/payment/transactions", h.GetTransactions)
	mux.HandleFunc("GET /api/v1/payment/transaction/{id}", h.GetTransaction)
	// Webhooks (no auth - verified by signature)
	mux.HandleFunc("POST /api/v1/payment/webhook/razorpay", h.RazorpayWebhook)
	mux.HandleFunc("POST /api/v1/payment/webhook/crypto", h.CryptoWebhook)
}

func (h *Handler) UPIDeposit(w http.ResponseWriter, r *http.Request) {
	var req UPIDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	tx, err := h.service.InitiateUPIDeposit(r.Context(), userID, &req)
	if err != nil {
		h.service.logger.Error("UPI deposit initiation failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusBadRequest, "failed to initiate UPI deposit")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, tx)
}

func (h *Handler) CryptoDeposit(w http.ResponseWriter, r *http.Request) {
	var req CryptoDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	tx, err := h.service.InitiateCryptoDeposit(r.Context(), userID, &req)
	if err != nil {
		h.service.logger.Error("crypto deposit initiation failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusBadRequest, "failed to initiate crypto deposit")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, tx)
}

func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	var req WithdrawalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	tx, err := h.service.InitiateWithdrawal(r.Context(), userID, &req)
	if err != nil {
		h.service.logger.Error("withdrawal initiation failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusBadRequest, "failed to initiate withdrawal")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, tx)
}

func (h *Handler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	txns, err := h.service.GetUserTransactions(r.Context(), userID, limit, offset)
	if err != nil {
		h.service.logger.Error("get transactions failed", "error", err, "user_id", userID)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to retrieve transactions")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, txns)
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("id")
	tx, err := h.service.GetTransaction(r.Context(), txID)
	if err != nil {
		h.service.logger.Error("get transaction failed", "error", err, "tx_id", txID)
		httputil.WriteError(w, http.StatusNotFound, "transaction not found")
		return
	}

	// Verify ownership
	userID := middleware.UserIDFromContext(r.Context())
	if tx.UserID != userID {
		httputil.WriteError(w, http.StatusForbidden, "not authorized")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, tx)
}

func (h *Handler) RazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	signature := r.Header.Get("X-Razorpay-Signature")
	if !h.service.verifyRazorpaySignature(body, signature) {
		h.service.logger.Warn("razorpay webhook: invalid signature")
		httputil.WriteError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// After signature verification succeeds, always return 200 to prevent
	// retry storms from the payment provider. Log errors server-side.
	if err := h.service.HandleRazorpayWebhook(r.Context(), body, signature); err != nil {
		h.service.logger.Error("razorpay webhook processing failed", "error", err)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CryptoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Verify HMAC-SHA256 signature before processing the webhook.
	signature := r.Header.Get("X-Webhook-Signature")
	if !h.service.VerifyCryptoSignature(body, signature) {
		h.service.logger.Warn("crypto webhook: invalid signature")
		httputil.WriteError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	var webhook CryptoWebhook
	if err := json.Unmarshal(body, &webhook); err != nil {
		// Signature was valid but body is malformed -- return 200 to avoid retries.
		h.service.logger.Error("crypto webhook: invalid JSON body", "error", err)
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// After signature verification succeeds, always return 200 to prevent
	// retry storms from the payment provider. Log errors server-side.
	if err := h.service.HandleCryptoWebhook(r.Context(), &webhook); err != nil {
		h.service.logger.Error("crypto webhook processing failed", "error", err)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
