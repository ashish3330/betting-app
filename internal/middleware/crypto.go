package middleware

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

var encryptionKey []byte

// aead is the AES-GCM cipher created once at init and reused for all
// encrypt/decrypt operations.  cipher.AEAD is safe for concurrent use.
var aead cipher.AEAD

// bufPool recycles bytes.Buffer instances used to capture downstream
// response bodies so we avoid an allocation per request.
var bufPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

// isTestBinary reports whether the running binary is a `go test` test binary.
// We use this to allow init() to fall back to a deterministic placeholder
// secret when ENCRYPTION_SECRET is unset under `go test`. Production binaries
// (gateway, services, etc.) are unaffected and still hard-fail at startup.
func isTestBinary() bool {
	return strings.HasSuffix(os.Args[0], ".test") ||
		strings.Contains(os.Args[0], "/_test/") ||
		strings.Contains(os.Args[0], "go-build")
}

func init() {
	secret := os.Getenv("ENCRYPTION_SECRET")
	if secret == "" {
		if isTestBinary() {
			// Deterministic, well-known test placeholder. Never used in
			// production paths because production binaries set the env
			// var explicitly and never satisfy isTestBinary().
			secret = "test-only-placeholder-encryption-secret"
		} else {
			fmt.Fprintln(os.Stderr, "FATAL: ENCRYPTION_SECRET environment variable is required")
			os.Exit(1)
		}
	}
	if len(secret) < 16 {
		fmt.Fprintln(os.Stderr, "FATAL: ENCRYPTION_SECRET must be at least 16 characters")
		os.Exit(1)
	}
	hash := sha256.Sum256([]byte(secret))
	encryptionKey = hash[:]

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to create AES cipher: %v\n", err)
		os.Exit(1)
	}
	aead, err = cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to create GCM: %v\n", err)
		os.Exit(1)
	}
}

func decryptPayload(encrypted string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}

	nonceSize := aead.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return aead.Open(nil, nonce, ciphertext, nil)
}

// encryptBytes encrypts an already-serialized JSON payload directly, avoiding
// the json.Marshal → []byte round-trip that was previously necessary when the
// caller handed us an arbitrary interface{}.
func encryptBytes(plaintext []byte) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// encryptPayload is retained for callers that still hand us a Go value.
func encryptPayload(data interface{}) (string, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return encryptBytes(plaintext)
}

type encryptedResponseWriter struct {
	http.ResponseWriter
	body          *bytes.Buffer
	statusCode    int
	headerWritten bool
}

func (w *encryptedResponseWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true
	w.statusCode = code
}

func (w *encryptedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

// Unwrap supports http.ResponseController and middleware that check for
// wrapped writers (e.g. http.Flusher).
func (w *encryptedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// EncryptionMiddleware decrypts incoming encrypted request bodies and encrypts
// all JSON responses. This matches the frontend's AES-256-GCM crypto layer.
func EncryptionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip encryption for health, websocket, and SSE endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/healthz" || r.URL.Path == "/ws" || strings.HasSuffix(r.URL.Path, "/stream") {
			next.ServeHTTP(w, r)
			return
		}

		// Decrypt incoming POST/PUT body if encrypted.  Peek the first byte
		// before unmarshaling — if it's not '{' the body cannot be our
		// encryption envelope, so skip the expensive envelope parse.
		if (r.Method == "POST" || r.Method == "PUT") && r.Body != nil {
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				bodyBytes, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err == nil && len(bodyBytes) > 0 {
					trimmed := bytes.TrimLeft(bodyBytes, " \t\r\n")
					if len(trimmed) > 0 && trimmed[0] == '{' && bytes.Contains(trimmed[:min(16, len(trimmed))], []byte(`"d"`)) {
						var envelope struct {
							D string `json:"d"`
						}
						if json.Unmarshal(bodyBytes, &envelope) == nil && envelope.D != "" {
							decrypted, err := decryptPayload(envelope.D)
							if err == nil {
								bodyBytes = decrypted
							}
							// If decryption fails, fall through with original body
						}
					}
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				} else {
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}
			}
		}

		// Capture the response into a pooled buffer to avoid allocating a
		// fresh bytes.Buffer on every request.
		buf := bufPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			// Clip excessively-grown buffers before returning them to the pool
			// so a single large response doesn't pin memory forever.
			if buf.Cap() > 64*1024 {
				buf = new(bytes.Buffer)
			}
			bufPool.Put(buf)
		}()

		erw := &encryptedResponseWriter{
			ResponseWriter: w,
			body:           buf,
			statusCode:     200,
		}

		next.ServeHTTP(erw, r)

		// The captured body is already valid JSON coming out of our handlers.
		// Skip the Unmarshal→Marshal round-trip entirely and encrypt the raw
		// bytes directly. Hand-write the envelope so we avoid a json.Encoder
		// allocation and a third JSON traversal.
		responseBody := buf.Bytes()
		if len(responseBody) > 0 && looksLikeJSON(responseBody) {
			encrypted, err := encryptBytes(responseBody)
			if err == nil {
				h := w.Header()
				h.Set("Content-Type", "application/json")
				h.Set("X-Encrypted", "true")
				w.WriteHeader(erw.statusCode)
				// {"d":"<base64>"}
				w.Write([]byte(`{"d":"`))
				w.Write([]byte(encrypted))
				w.Write([]byte(`"}`))
				return
			}
		}

		// Fallback: write original response
		w.WriteHeader(erw.statusCode)
		w.Write(responseBody)
	})
}

// looksLikeJSON does a cheap prefix check to decide whether the captured
// response is JSON that we should encrypt. Handlers occasionally write plain
// text (e.g. error bodies from http.Error); those should pass through
// unmodified.
func looksLikeJSON(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', '[', '"':
			return true
		default:
			return false
		}
	}
	return false
}

