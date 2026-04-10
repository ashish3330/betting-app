//go:build ignore

// test_encrypt.go sends an AES-256-GCM encrypted login request to the gateway.
//
// Required environment variables:
//   ENCRYPTION_SECRET  - AES secret shared with the backend
//   TEST_USERNAME      - Username for the login request
//   TEST_PASSWORD      - Password for the login request
//   API_URL            - (optional) Gateway URL, defaults to http://localhost:8080
//
// Usage:
//   ENCRYPTION_SECRET="your-secret" TEST_USERNAME="player1" TEST_PASSWORD="Pass@123" \
//     go run scripts/test_encrypt.go

package main

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
)

func requiredEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "error: %s environment variable is required\n", key)
		os.Exit(1)
	}
	return v
}

func main() {
	secret := requiredEnv("ENCRYPTION_SECRET")
	username := requiredEnv("TEST_USERNAME")
	password := requiredEnv("TEST_PASSWORD")

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	hash := sha256.Sum256([]byte(secret))
	key := hash[:]

	body := map[string]string{"username": username, "password": password}
	plaintext, err := json.Marshal(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling body: %v\n", err)
		os.Exit(1)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating cipher: %v\n", err)
		os.Exit(1)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating GCM: %v\n", err)
		os.Exit(1)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		fmt.Fprintf(os.Stderr, "error generating nonce: %v\n", err)
		os.Exit(1)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	encrypted := base64.StdEncoding.EncodeToString(ciphertext)

	envelope := map[string]string{"d": encrypted}
	envJSON, _ := json.Marshal(envelope)

	resp, err := http.Post(apiURL+"/api/v1/auth/login", "application/json", bytes.NewReader(envJSON))
	if err != nil {
		fmt.Fprintf(os.Stderr, "request error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Println("HTTP Status:", resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) > 200 {
		fmt.Println("Response:", string(respBody[:200]), "...")
	} else {
		fmt.Println("Response:", string(respBody))
	}
}
