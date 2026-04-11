package httputil

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
)

// encoderBufPool recycles the intermediate buffer used by WriteJSON so we
// avoid allocating a fresh bytes.Buffer and json.Encoder on every response.
var encoderBufPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

// WriteJSON serializes v as JSON with the given status code.
// On marshal failure, writes a 500 with a generic error message.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if v == nil {
		w.WriteHeader(status)
		return
	}

	buf := encoderBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// Clip excessively grown buffers before returning them to the pool
		// so a single fat response doesn't pin memory forever.
		if buf.Cap() > 64*1024 {
			buf = new(bytes.Buffer)
		}
		encoderBufPool.Put(buf)
	}()

	enc := json.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		slog.Error("httputil.WriteJSON encode failed", "error", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
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
