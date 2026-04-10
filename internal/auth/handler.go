package auth

import (
	"encoding/json"
	"net/http"
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
