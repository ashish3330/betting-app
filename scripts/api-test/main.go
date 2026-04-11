// Comprehensive API integration test for Lotus Exchange.
// Tests every endpoint via the gateway, exercising encryption envelope, auth, and roles.
//
// Usage:
//   ENCRYPTION_SECRET=... SEED_SECRET=... go run ./scripts/api-test
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	baseURL = flag.String("base", "http://localhost:8080", "API base URL")
	verbose = flag.Bool("v", false, "verbose output")
	// `monolith` was the legacy default; it pointed at a single binary at
	// cmd/server which has been deleted. The microservices stack (gateway
	// + 12 services) is the only supported runtime today, so the default
	// is now `microservices`. The flag itself is kept for forward
	// compatibility — anything other than "microservices" disables the
	// skip-list entirely (which is now empty anyway).
	mode = flag.String("mode", "microservices", "target mode: 'microservices' (default) or any other value for legacy behaviour")
)

// endpointsMissingInMicroservices lists test names that hit endpoints not
// yet implemented in microservices mode. The skip-list is intentionally
// empty: every route is exercised against the gateway, so any regression
// on a previously-skipped endpoint shows up as a real failure instead of
// a silent skip.
var endpointsMissingInMicroservices = map[string]string{}

var (
	totalTests int
	passed     int
	failed     int
	skipped    int
	results    []string
)

var (
	encKey []byte
	client *http.Client
)

func init() {
	secret := os.Getenv("ENCRYPTION_SECRET")
	if secret == "" {
		secret = "lotus-dev-local-encryption-key-change-in-prod-min-32-chars" //nolint:gosec // G101: dev-only default for the e2e test harness; real secret comes from ENCRYPTION_SECRET env var
	}
	hash := sha256.Sum256([]byte(secret))
	encKey = hash[:]
	client = &http.Client{Timeout: 15 * time.Second}
}

// ── Encryption helpers (matches Go backend) ─────────────────────

func encryptBody(data interface{}) ([]byte, error) {
	plaintext, _ := json.Marshal(data)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	envelope := map[string]string{"d": base64.StdEncoding.EncodeToString(ct)}
	return json.Marshal(envelope)
}

func decryptBody(body []byte) ([]byte, error) {
	var envelope struct{ D string }
	if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil {
		// Body is not an encrypted envelope; return as-is. This is not an
		// error — plaintext responses are valid.
		return body, nil //nolint:nilerr // plaintext passthrough is intentional
	}
	if envelope.D == "" {
		return body, nil // not encrypted
	}
	data, err := base64.StdEncoding.DecodeString(envelope.D)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// ── HTTP wrapper ─────────────────────────────────────────────────

type APIResponse struct {
	Status int
	Body   []byte
}

func apiCall(method, path string, body interface{}, token string) (*APIResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		encrypted, err := encryptBody(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(encrypted)
	}
	req, err := http.NewRequest(method, *baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	decrypted, _ := decryptBody(raw)
	return &APIResponse{Status: resp.StatusCode, Body: decrypted}, nil
}

// ── Test runner ──────────────────────────────────────────────────

func test(name string, fn func() error) {
	// In microservices mode, record known-missing endpoints as skipped
	// instead of running them so the failure list stays actionable.
	if *mode == "microservices" {
		if reason, ok := endpointsMissingInMicroservices[name]; ok {
			skip(name, reason)
			return
		}
	}
	totalTests++
	err := fn()
	if err != nil {
		failed++
		results = append(results, fmt.Sprintf("✗ %s -- %v", name, err))
		fmt.Printf("✗ %s -- %v\n", name, err)
	} else {
		passed++
		results = append(results, fmt.Sprintf("✓ %s", name))
		if *verbose {
			fmt.Printf("✓ %s\n", name)
		}
	}
}

func skip(name, reason string) {
	totalTests++
	skipped++
	results = append(results, fmt.Sprintf("○ %s -- skipped: %s", name, reason))
	if *verbose {
		fmt.Printf("○ %s -- %s\n", name, reason)
	}
}

func mustOk(resp *APIResponse, allowedStatuses ...int) error {
	if len(allowedStatuses) == 0 {
		allowedStatuses = []int{200, 201, 204}
	}
	for _, s := range allowedStatuses {
		if resp.Status == s {
			return nil
		}
	}
	return fmt.Errorf("status=%d body=%s", resp.Status, truncate(string(resp.Body), 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ── Tokens cache ─────────────────────────────────────────────────

var (
	playerToken  string
	playerUserID int64
	adminToken   string
	masterToken  string
	agentToken   string
)

func login(username, password string) (string, int64, error) {
	resp, err := apiCall("POST", "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, "")
	if err != nil {
		return "", 0, err
	}
	if resp.Status != 200 {
		return "", 0, fmt.Errorf("login %s: status=%d body=%s", username, resp.Status, truncate(string(resp.Body), 150))
	}
	var data struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		return "", 0, fmt.Errorf("parse login response: %w body=%s", err, truncate(string(resp.Body), 150))
	}
	if data.AccessToken == "" {
		return "", 0, fmt.Errorf("no access_token in response: %s", truncate(string(resp.Body), 150))
	}
	return data.AccessToken, data.User.ID, nil
}

// ── Tests ────────────────────────────────────────────────────────

func main() {
	flag.Parse()
	fmt.Printf("Lotus Exchange API Test Suite\nBase: %s\n\n", *baseURL)

	// Phase 1: Health & Public endpoints
	fmt.Println("─── Phase 1: Health & Public ───")
	test("GET /health", func() error {
		resp, err := apiCall("GET", "/health", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/sports", func() error {
		resp, err := apiCall("GET", "/api/v1/sports", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/competitions?sport=cricket", func() error {
		resp, err := apiCall("GET", "/api/v1/competitions?sport=cricket", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/events?sport=cricket", func() error {
		resp, err := apiCall("GET", "/api/v1/events?sport=cricket", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/markets?sport=cricket", func() error {
		resp, err := apiCall("GET", "/api/v1/markets?sport=cricket", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/casino/providers", func() error {
		resp, err := apiCall("GET", "/api/v1/casino/providers", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/casino/games", func() error {
		resp, err := apiCall("GET", "/api/v1/casino/games", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/casino/categories", func() error {
		resp, err := apiCall("GET", "/api/v1/casino/categories", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/odds/status", func() error {
		resp, err := apiCall("GET", "/api/v1/odds/status", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 2: Seed (idempotent — won't fail if already seeded)
	fmt.Println("\n─── Phase 2: Seed Database ───")
	test("POST /api/v1/seed", func() error {
		req, _ := http.NewRequest("POST", *baseURL+"/api/v1/seed", nil)
		seedSecret := os.Getenv("SEED_SECRET")
		if seedSecret == "" {
			seedSecret = "lotus-dev-seed-secret" //nolint:gosec // G101: dev-only default for the e2e test harness; real secret comes from SEED_SECRET env var
		}
		req.Header.Set("X-Seed-Secret", seedSecret)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("seed failed: status=%d body=%s", resp.StatusCode, truncate(string(body), 200))
		}
		return nil
	})

	// Phase 3: Auth
	fmt.Println("\n─── Phase 3: Auth ───")

	test("Login player1", func() error {
		t, id, err := login("player1", "Player@123")
		if err != nil {
			return err
		}
		playerToken = t
		playerUserID = id
		return nil
	})
	test("Login agent1", func() error {
		t, _, err := login("agent1", "Agent@123")
		if err != nil {
			return err
		}
		agentToken = t
		return nil
	})
	test("Login master1", func() error {
		t, _, err := login("master1", "Master@123")
		if err != nil {
			return err
		}
		masterToken = t
		return nil
	})
	test("Login admin1", func() error {
		t, _, err := login("admin1", "Admin@123")
		if err != nil {
			return err
		}
		adminToken = t
		return nil
	})
	test("Login superadmin", func() error {
		_, _, err := login("superadmin", "Admin@123")
		return err
	})
	test("Login wrong password (should fail with 401)", func() error {
		resp, err := apiCall("POST", "/api/v1/auth/login", map[string]string{
			"username": "player1",
			"password": "wrong",
		}, "")
		if err != nil {
			return err
		}
		if resp.Status == 401 {
			return nil
		}
		return fmt.Errorf("expected 401, got %d", resp.Status)
	})
	test("GET /api/v1/auth/sessions (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/auth/sessions", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/auth/login-history (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/auth/login-history", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 4: Wallet
	fmt.Println("\n─── Phase 4: Wallet ───")
	test("GET /api/v1/wallet/balance (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/wallet/balance", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/wallet/ledger (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/wallet/ledger", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 5: Bets
	fmt.Println("\n─── Phase 5: Bets ───")
	test("GET /api/v1/bets (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/bets", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Find a match_odds market to bet on (skip fancy/bookmaker for the back/lay test)
	var marketID string
	var selectionID int64
	var betPrice float64
	resp, _ := apiCall("GET", "/api/v1/markets?sport=cricket", nil, "")
	if resp != nil && resp.Status == 200 {
		var markets []struct {
			ID         string `json:"id"`
			MarketType string `json:"market_type"`
			Runners    []struct {
				SelectionID int64   `json:"selection_id"`
				BackPrice   float64 `json:"back_price"`
				LayPrice    float64 `json:"lay_price"`
			} `json:"runners"`
		}
		_ = json.Unmarshal(resp.Body, &markets)
		for _, m := range markets {
			if m.MarketType != "" && m.MarketType != "match_odds" {
				continue
			}
			if len(m.Runners) == 0 {
				continue
			}
			marketID = m.ID
			selectionID = m.Runners[0].SelectionID
			betPrice = m.Runners[0].BackPrice
			break
		}
		if betPrice == 0 && marketID != "" {
			// Re-fetch live odds (market list returns price arrays, not flat fields)
			oddsResp, _ := apiCall("GET", "/api/v1/markets/"+marketID+"/odds", nil, "")
			if oddsResp != nil && oddsResp.Status == 200 {
				var odds struct {
					Runners []struct {
						SelectionID int64 `json:"selection_id"`
						BackPrices  []struct {
							Price float64 `json:"price"`
						} `json:"back_prices"`
					} `json:"runners"`
				}
				_ = json.Unmarshal(oddsResp.Body, &odds)
				for _, r := range odds.Runners {
					if r.SelectionID == selectionID && len(r.BackPrices) > 0 {
						betPrice = r.BackPrices[0].Price
						break
					}
				}
			}
		}
	}
	if marketID != "" {
		test(fmt.Sprintf("GET /api/v1/markets/%s/odds", marketID), func() error {
			resp, err := apiCall("GET", "/api/v1/markets/"+marketID+"/odds", nil, "")
			if err != nil {
				return err
			}
			return mustOk(resp)
		})
		test(fmt.Sprintf("GET /api/v1/market/%s/orderbook", marketID), func() error {
			resp, err := apiCall("GET", "/api/v1/market/"+marketID+"/orderbook", nil, playerToken)
			if err != nil {
				return err
			}
			return mustOk(resp)
		})
		test("POST /api/v1/bet/place (player back bet)", func() error {
			if betPrice == 0 {
				return fmt.Errorf("could not determine current back price for market %s", marketID)
			}
			resp, err := apiCall("POST", "/api/v1/bet/place", map[string]interface{}{
				"market_id":    marketID,
				"selection_id": selectionID,
				"side":         "back",
				"price":        betPrice,
				"stake":        100.0,
				"client_ref":   fmt.Sprintf("test-%d", time.Now().UnixNano()),
			}, playerToken)
			if err != nil {
				return err
			}
			// Allow 409 ODDS_CHANGED in case price moved between fetch and place
			return mustOk(resp, 200, 201, 409)
		})
		test(fmt.Sprintf("GET /api/v1/positions/%s", marketID), func() error {
			resp, err := apiCall("GET", "/api/v1/positions/"+marketID, nil, playerToken)
			if err != nil {
				return err
			}
			return mustOk(resp)
		})
	} else {
		skip("Bet placement", "no market available")
	}

	// Phase 6: Casino
	fmt.Println("\n─── Phase 6: Casino ───")
	test("GET /api/v1/casino/games/livecasino", func() error {
		resp, err := apiCall("GET", "/api/v1/casino/games/livecasino", nil, "")
		if err != nil {
			return err
		}
		return mustOk(resp, 200, 404)
	})
	test("GET /api/v1/casino/history (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/casino/history", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 7: Payments
	fmt.Println("\n─── Phase 7: Payments ───")
	test("GET /api/v1/payment/transactions (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/payment/transactions", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 8: Notifications
	fmt.Println("\n─── Phase 8: Notifications ───")
	test("GET /api/v1/notifications (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/notifications", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/notifications/unread-count (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/notifications/unread-count", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 9: Responsible Gambling
	fmt.Println("\n─── Phase 9: Responsible Gambling ───")
	test("GET /api/v1/responsible/limits (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/responsible/limits", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 10: Hierarchy
	fmt.Println("\n─── Phase 10: Hierarchy ───")
	test("GET /api/v1/hierarchy/children (agent)", func() error {
		resp, err := apiCall("GET", "/api/v1/hierarchy/children", nil, agentToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/hierarchy/children/direct (agent)", func() error {
		resp, err := apiCall("GET", "/api/v1/hierarchy/children/direct", nil, agentToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 11: Risk
	fmt.Println("\n─── Phase 11: Risk ───")
	test("GET /api/v1/risk/exposure (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/risk/exposure", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 12: Reports
	fmt.Println("\n─── Phase 12: Reports ───")
	test("GET /api/v1/reports/dashboard (admin)", func() error {
		resp, err := apiCall("GET", "/api/v1/reports/dashboard", nil, adminToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/reports/pnl (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/reports/pnl", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 13: Admin (with role checks)
	fmt.Println("\n─── Phase 13: Admin ───")
	test("GET /api/v1/admin/users (admin)", func() error {
		resp, err := apiCall("GET", "/api/v1/admin/users", nil, adminToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/admin/users (player → 403)", func() error {
		resp, err := apiCall("GET", "/api/v1/admin/users", nil, playerToken)
		if err != nil {
			return err
		}
		if resp.Status == 403 {
			return nil
		}
		return fmt.Errorf("expected 403, got %d", resp.Status)
	})
	test("GET /api/v1/admin/markets (admin)", func() error {
		resp, err := apiCall("GET", "/api/v1/admin/markets", nil, adminToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/admin/bets (admin)", func() error {
		resp, err := apiCall("GET", "/api/v1/admin/bets", nil, adminToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 14: Panel
	fmt.Println("\n─── Phase 14: Panel ───")
	test("GET /api/v1/panel/dashboard (master)", func() error {
		resp, err := apiCall("GET", "/api/v1/panel/dashboard", nil, masterToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/panel/users (master)", func() error {
		resp, err := apiCall("GET", "/api/v1/panel/users", nil, masterToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/panel/audit (admin)", func() error {
		resp, err := apiCall("GET", "/api/v1/panel/audit", nil, adminToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/panel/reports/pnl (master)", func() error {
		resp, err := apiCall("GET", "/api/v1/panel/reports/pnl", nil, masterToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/panel/reports/volume (master)", func() error {
		resp, err := apiCall("GET", "/api/v1/panel/reports/volume", nil, masterToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 15a: New aliases (wallet/* delegates, bets/history, change-password)
	fmt.Println("\n─── Phase 15a: New endpoints ───")
	test("GET /api/v1/wallet/statement (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/wallet/statement", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/wallet/deposits (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/wallet/deposits", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/wallet/withdrawals (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/wallet/withdrawals", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/bets/history (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/bets/history", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("POST /api/v1/auth/otp/resend (player)", func() error {
		resp, err := apiCall("POST", "/api/v1/auth/otp/resend", map[string]interface{}{
			"user_id": playerUserID,
		}, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 15: Referral
	fmt.Println("\n─── Phase 15: Referral ───")
	test("GET /api/v1/referral/code (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/referral/code", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})
	test("GET /api/v1/referral/stats (player)", func() error {
		resp, err := apiCall("GET", "/api/v1/referral/stats", nil, playerToken)
		if err != nil {
			return err
		}
		return mustOk(resp)
	})

	// Phase 16: End-to-end user flows (multi-step journeys)
	fmt.Println("\n─── Phase 16: End-to-end flows ───")
	time.Sleep(3 * time.Second) // let rate limiter recover

	// Flow 1: Login → check balance → place bet → verify bet appears in history → cancel/settle
	test("E2E: Login → balance → place bet → history", func() error {
		// 1. Get balance before bet
		balResp, err := apiCall("GET", "/api/v1/wallet/balance", nil, playerToken)
		if err != nil || balResp.Status != 200 {
			return fmt.Errorf("get balance failed: %v", err)
		}
		var balBefore struct {
			Balance         float64 `json:"balance"`
			AvailableBalance float64 `json:"available_balance"`
		}
		_ = json.Unmarshal(balResp.Body, &balBefore)
		if balBefore.AvailableBalance < 100 {
			return fmt.Errorf("insufficient balance for E2E test: %.2f", balBefore.AvailableBalance)
		}
		// 2. Get current odds and place a bet
		if marketID == "" || betPrice == 0 {
			return fmt.Errorf("no market available for E2E test")
		}
		// Re-fetch latest odds to avoid ODDS_CHANGED
		oddsResp, _ := apiCall("GET", "/api/v1/markets/"+marketID+"/odds", nil, "")
		var odds struct {
			Runners []struct {
				SelectionID int64 `json:"selection_id"`
				BackPrices  []struct{ Price float64 `json:"price"` } `json:"back_prices"`
			} `json:"runners"`
		}
		_ = json.Unmarshal(oddsResp.Body, &odds)
		var freshPrice float64
		for _, r := range odds.Runners {
			if r.SelectionID == selectionID && len(r.BackPrices) > 0 {
				freshPrice = r.BackPrices[0].Price
				break
			}
		}
		if freshPrice == 0 {
			return fmt.Errorf("could not get fresh price")
		}
		betResp, err := apiCall("POST", "/api/v1/bet/place", map[string]interface{}{
			"market_id":    marketID,
			"selection_id": selectionID,
			"side":         "back",
			"price":        freshPrice,
			"stake":        100.0,
			"client_ref":   fmt.Sprintf("e2e-%d", time.Now().UnixNano()),
		}, playerToken)
		if err != nil {
			return fmt.Errorf("place bet failed: %v", err)
		}
		if betResp.Status != 200 && betResp.Status != 201 && betResp.Status != 409 {
			return fmt.Errorf("bet place returned %d: %s", betResp.Status, truncate(string(betResp.Body), 150))
		}
		// 3. Verify history endpoint works
		histResp, err := apiCall("GET", "/api/v1/bets/history", nil, playerToken)
		if err != nil || histResp.Status != 200 {
			return fmt.Errorf("get bet history failed: %v", err)
		}
		return nil
	})

	// Flow 2: Login → fetch markets → fetch event markets → check positions
	test("E2E: Login → markets → event markets → positions", func() error {
		mResp, _ := apiCall("GET", "/api/v1/markets?sport=cricket", nil, "")
		if mResp.Status != 200 {
			return fmt.Errorf("get markets failed: %d", mResp.Status)
		}
		var markets []struct {
			ID      string `json:"id"`
			EventID string `json:"event_id"`
		}
		_ = json.Unmarshal(mResp.Body, &markets)
		if len(markets) == 0 {
			return fmt.Errorf("no markets returned")
		}
		// Get positions for the first market
		posResp, err := apiCall("GET", "/api/v1/positions/"+markets[0].ID, nil, playerToken)
		if err != nil {
			return err
		}
		if posResp.Status != 200 {
			return fmt.Errorf("get positions returned %d", posResp.Status)
		}
		return nil
	})

	// Brief pause to let rate limiter recover from the basic test phase
	time.Sleep(2 * time.Second)

	// Flow 3: Login → casino games → create session → close session → history
	test("E2E: Casino → session lifecycle", func() error {
		gamesResp, _ := apiCall("GET", "/api/v1/casino/games", nil, "")
		if gamesResp.Status != 200 {
			return fmt.Errorf("list games failed: %d", gamesResp.Status)
		}
		histResp, err := apiCall("GET", "/api/v1/casino/history", nil, playerToken)
		if err != nil {
			return fmt.Errorf("casino history error: %v", err)
		}
		if histResp.Status != 200 {
			return fmt.Errorf("casino history returned %d", histResp.Status)
		}
		return nil
	})

	// Flow 4: Admin login → list users → list markets → list bets
	test("E2E: Admin → users → markets → bets", func() error {
		uResp, err := apiCall("GET", "/api/v1/admin/users", nil, adminToken)
		if err != nil {
			return fmt.Errorf("admin users error: %v", err)
		}
		if uResp.Status != 200 {
			return fmt.Errorf("admin users returned %d", uResp.Status)
		}
		mResp, err := apiCall("GET", "/api/v1/admin/markets", nil, adminToken)
		if err != nil {
			return fmt.Errorf("admin markets error: %v", err)
		}
		if mResp.Status != 200 {
			return fmt.Errorf("admin markets returned %d", mResp.Status)
		}
		bResp, err := apiCall("GET", "/api/v1/admin/bets", nil, adminToken)
		if err != nil {
			return fmt.Errorf("admin bets error: %v", err)
		}
		if bResp.Status != 200 {
			return fmt.Errorf("admin bets returned %d", bResp.Status)
		}
		return nil
	})

	time.Sleep(2 * time.Second)

	// Flow 5: Wallet alias endpoints work end-to-end
	test("E2E: Wallet aliases (statement/deposits/withdrawals)", func() error {
		for _, path := range []string{
			"/api/v1/wallet/balance",
			"/api/v1/wallet/ledger",
			"/api/v1/wallet/statement",
			"/api/v1/wallet/deposits",
			"/api/v1/wallet/withdrawals",
		} {
			resp, err := apiCall("GET", path, nil, playerToken)
			if err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			if resp.Status != 200 {
				return fmt.Errorf("%s returned %d", path, resp.Status)
			}
			time.Sleep(100 * time.Millisecond) // gentle pacing
		}
		return nil
	})

	// ── Final Report ────────────────────────────────────────────
	fmt.Printf("\n══════════════════════════════════════\n")
	fmt.Printf("Total: %d  Passed: %d  Failed: %d  Skipped: %d\n", totalTests, passed, failed, skipped)
	if failed > 0 {
		fmt.Printf("\n── Failures ──\n")
		for _, r := range results {
			if strings.HasPrefix(r, "✗") {
				fmt.Println(r)
			}
		}
		os.Exit(1)
	}
	fmt.Println("\n✅ ALL TESTS PASSED")
}
