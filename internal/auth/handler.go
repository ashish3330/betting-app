package auth

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.Refresh)
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Public registration always creates a "client" role.
	// Only admin/panel endpoints may assign other roles.
	req.Role = "client"

	user, err := h.service.Register(r.Context(), &req)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.Login(r.Context(), &req)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		httputil.WriteError(w, http.StatusUnauthorized, "missing token")
		return
	}

	if err := h.service.Logout(r.Context(), token); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func (h *Handler) OTPVerify(w http.ResponseWriter, r *http.Request) {
	// TODO: implement OTP verification
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "otp verified"})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	// TODO: implement password change
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

func (h *Handler) OTPGenerate(w http.ResponseWriter, r *http.Request) {
	// TODO: implement OTP generation
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "otp generated"})
}

func (h *Handler) OTPEnable(w http.ResponseWriter, r *http.Request) {
	// TODO: implement OTP enable
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "otp enabled"})
}

// authenticatedUserID re-validates the bearer token and returns the user
// ID encoded in its claims. AuthMiddleware has already validated the
// token before these handlers run, but importing the middleware package
// here would introduce a circular dependency, so we revalidate via the
// token cache-backed ValidateToken path (which is cheap for warmed
// tokens).
func (h *Handler) authenticatedUserID(r *http.Request) (int64, bool) {
	token := extractToken(r)
	if token == "" {
		return 0, false
	}
	claims, err := h.service.ValidateToken(token)
	if err != nil {
		return 0, false
	}
	return claims.UserID, true
}

// GetSessions returns the active sessions for the authenticated user.
// Ported from cmd/server/main.go:handleGetSessions. The most recent
// successful login is flagged current=true.
func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.authenticatedUserID(r)
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sessions, err := h.service.GetActiveSessions(r.Context(), uid)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []*SessionInfo{}
	}
	httputil.WriteJSON(w, http.StatusOK, sessions)
}

// LoginHistory returns the last N login records for the authenticated
// user. Ported from cmd/server/main.go:handleLoginHistory. The `limit`
// query parameter defaults to 20 and is capped at 500 by the service.
func (h *Handler) LoginHistory(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.authenticatedUserID(r)
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	records, err := h.service.GetLoginHistory(r.Context(), uid, limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []*LoginRecord{}
	}
	httputil.WriteJSON(w, http.StatusOK, records)
}

// OTPResend issues a fresh OTP code for a user. This endpoint is PUBLIC
// (used during the pre-login OTP flow) so it must not expose whether a
// user_id exists — all failures return the same 200 body. Ported from
// cmd/server/main.go:handleOTPResend.
func (h *Handler) OTPResend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64 `json:"user_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// A valid user_id is required. We return 400 only when the caller
	// forgot the field entirely — unknown IDs still return 200 below to
	// prevent enumeration.
	if req.UserID <= 0 {
		httputil.WriteError(w, http.StatusBadRequest, "user_id required")
		return
	}

	if _, err := h.service.ResendOTP(r.Context(), req.UserID); err != nil {
		// Swallow the error so the response is indistinguishable from
		// the success path. The service has already logged the detail.
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "OTP sent"})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "OTP sent"})
}

// LogoutAllSessions revokes every refresh token belonging to the
// authenticated user. Ported from cmd/server/main.go:handleLogoutAllSessions.
func (h *Handler) LogoutAllSessions(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.authenticatedUserID(r)
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	count, err := h.service.RevokeAllRefreshTokens(r.Context(), uid)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "all sessions terminated",
		"revoked": count,
	})
}
