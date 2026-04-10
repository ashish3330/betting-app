package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON serializes v as JSON with the given status code.
// On marshal failure, writes a 500 with a generic error message.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("httputil.WriteJSON encode failed", "error", err)
	}
}

// WriteError writes a JSON error response: {"error": "<message>"}.
// Use generic messages — never leak internal error details to clients.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// WriteErrorWithCode writes a JSON error with a machine-readable code:
// {"error": "<message>", "code": "<code>"}
func WriteErrorWithCode(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]string{"error": message, "code": code})
}
