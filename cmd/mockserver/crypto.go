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
)

var encryptionKey []byte

func initEncryption() {
	secret := os.Getenv("ENCRYPTION_SECRET")
	if secret == "" {
		secret = "lotus-exchange-2026-aes-secret-key" // fallback for dev
	}
	hash := sha256.Sum256([]byte(secret))
	encryptionKey = hash[:]
}

// Encrypt JSON data with AES-256-GCM
func encryptPayload(data interface{}) (string, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt AES-256-GCM encrypted base64 string
func decryptPayload(encrypted string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return aesGCM.Open(nil, nonce, ciphertext, nil)
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

		// Decrypt incoming POST/PUT body if encrypted
		if (r.Method == "POST" || r.Method == "PUT") && r.Body != nil {
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				bodyBytes, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err == nil && len(bodyBytes) > 0 {
					// Check if body is encrypted: {"d":"..."}
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
			// Try to parse as JSON to encrypt
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

		// Fallback: write original response if encryption failed
		w.WriteHeader(erw.statusCode)
		w.Write(responseBody)
	})
}
