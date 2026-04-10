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
)

var encryptionKey []byte

// aead is the AES-GCM cipher created once at init and reused for all
// encrypt/decrypt operations.  cipher.AEAD is safe for concurrent use.
var aead cipher.AEAD

func init() {
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

func encryptPayload(data interface{}) (string, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
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

		// Decrypt incoming POST/PUT body if encrypted
		if (r.Method == "POST" || r.Method == "PUT") && r.Body != nil {
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				bodyBytes, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err == nil && len(bodyBytes) > 0 {
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
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				} else {
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}
			}
		}

		// Capture the response
		erw := &encryptedResponseWriter{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
			statusCode:     200,
		}

		next.ServeHTTP(erw, r)

		// Encrypt the captured response body
		responseBody := erw.body.Bytes()
		if len(responseBody) > 0 {
			var jsonData json.RawMessage
			if json.Unmarshal(responseBody, &jsonData) == nil {
				encrypted, err := encryptPayload(jsonData)
				if err == nil {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Encrypted", "true")
					w.WriteHeader(erw.statusCode)
					json.NewEncoder(w).Encode(map[string]string{"d": encrypted})
					return
				}
			}
		}

		// Fallback: write original response
		w.WriteHeader(erw.statusCode)
		w.Write(responseBody)
	})
}
