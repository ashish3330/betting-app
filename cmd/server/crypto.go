package main

// AES-256-GCM encryption/decryption for API payloads.
// The encryption key is derived from a shared secret using SHA-256.
// Frontend and backend share the same key.

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

// serverAEAD is the AES-GCM cipher created once in initEncryption and reused
// across all encrypt/decrypt calls.  cipher.AEAD is safe for concurrent use.
var serverAEAD cipher.AEAD

// serverBufPool recycles bytes.Buffer instances used to capture downstream
// response bodies. Pooling cuts an allocation per request on the hot path.
var serverBufPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

func initEncryption() {
	secret := os.Getenv("ENCRYPTION_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "FATAL: ENCRYPTION_SECRET environment variable is required")
		os.Exit(1)
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
	serverAEAD, err = cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to create GCM: %v\n", err)
		os.Exit(1)
	}
}

// encryptBytes encrypts an already-marshaled JSON payload with AES-256-GCM.
func encryptBytes(plaintext []byte) (string, error) {
	nonce := make([]byte, serverAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := serverAEAD.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Encrypt JSON data with AES-256-GCM
func encryptPayload(data interface{}) (string, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return encryptBytes(plaintext)
}

// Decrypt AES-256-GCM encrypted base64 string
func decryptPayload(encrypted string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}

	nonceSize := serverAEAD.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return serverAEAD.Open(nil, nonce, ciphertext, nil)
}

// encryptedResponseWriter captures the response body so it can be encrypted.
type encryptedResponseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	written    bool
}

func (w *encryptedResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.written = true
}

func (w *encryptedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

// looksLikeJSON does a cheap prefix check to decide whether the captured
// response is JSON that we should encrypt. Handlers occasionally write plain
// text (e.g. error bodies from http.Error); those pass through unmodified.
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

// encryptionMiddleware wraps all JSON responses with AES-256-GCM encryption.
// The /health endpoint is excluded so monitoring tools get plain JSON.
// The /api/v1/config endpoint is also excluded.
func encryptionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip encryption for health, config, CSV, seed, SSE, and odds status endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/api/v1/config" || r.URL.Path == "/api/v1/panel/reports/csv" || r.URL.Path == "/api/v1/seed" || r.URL.Path == "/api/v1/stream/odds" || r.URL.Path == "/api/v1/odds/status" {
			next.ServeHTTP(w, r)
			return
		}

		// Decrypt incoming POST/PUT body if encrypted.  Peek the first
		// non-whitespace byte — unless it is '{' the body cannot be our
		// envelope, so skip the JSON unmarshal entirely.
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

		// Capture the response into a pooled buffer so we avoid allocating
		// a fresh bytes.Buffer on every request.
		buf := serverBufPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			if buf.Cap() > 64*1024 {
				buf = new(bytes.Buffer)
			}
			serverBufPool.Put(buf)
		}()

		erw := &encryptedResponseWriter{
			ResponseWriter: w,
			body:           buf,
			statusCode:     200,
		}

		next.ServeHTTP(erw, r)

		// The handler's bytes are already valid JSON — skip the
		// Unmarshal→Marshal round-trip and encrypt the raw bytes.
		// Hand-write the {"d":"..."} envelope to avoid an encoder
		// allocation and a third JSON traversal.
		responseBody := buf.Bytes()
		if len(responseBody) > 0 && looksLikeJSON(responseBody) {
			encrypted, err := encryptBytes(responseBody)
			if err == nil {
				h := w.Header()
				h.Set("Content-Type", "application/json")
				h.Set("X-Encrypted", "true")
				w.WriteHeader(erw.statusCode)
				w.Write([]byte(`{"d":"`))
				w.Write([]byte(encrypted))
				w.Write([]byte(`"}`))
				return
			}
		}

		// Fallback: write original response if encryption failed
		w.WriteHeader(erw.statusCode)
		w.Write(responseBody)
	})
}
