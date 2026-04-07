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

func init() {
	secret := os.Getenv("ENCRYPTION_SECRET")
	if secret == "" {
		secret = "lotus-exchange-2026-aes-secret-key"
	}
	hash := sha256.Sum256([]byte(secret))
	encryptionKey = hash[:]
}

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

type encryptedResponseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *encryptedResponseWriter) WriteHeader(code int) {
	w.statusCode = code
}

func (w *encryptedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
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
