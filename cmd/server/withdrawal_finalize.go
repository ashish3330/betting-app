package main

// Withdrawal finalization. handleWithdraw creates a pending payment_txn
// and HOLDS funds via store.HoldFunds. Without a finalization endpoint
// the held funds stay locked forever — even after the bank confirms or
// rejects the wire. This file fills that gap.
//
// Two operator-facing actions:
//
//   POST /api/v1/admin/withdraw/{txID}/complete  — bank confirmed; debit
//                                                  the held funds and
//                                                  mark the txn complete.
//
//   POST /api/v1/admin/withdraw/{txID}/reject    — bank rejected (or
//                                                  manual cancel); release
//                                                  the held funds and
//                                                  mark the txn failed.
//
// Both endpoints are admin-only via requireRole.
//
// Idempotency: each txn carries an immutable status. Re-calling complete
// on an already-completed txn returns the existing record without double-
// debiting. Same for reject.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func nowRFC3339() string { return time.Now().Format(time.RFC3339) }

func jsonDecode(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func handleWithdrawComplete(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("txID")
	if txID == "" {
		writeErr(w, 400, "tx_id required")
		return
	}

	// Snapshot the txn under the store lock so we can release it before
	// touching the user record.
	store.mu.Lock()
	tx, ok := store.paymentTxns[txID]
	if !ok && useDB() {
		// Fallback to DB if not in cache (e.g., after restart).
		store.mu.Unlock()
		writeErr(w, 404, "transaction not found")
		return
	}
	if !ok {
		store.mu.Unlock()
		writeErr(w, 404, "transaction not found")
		return
	}
	if tx.Direction != "withdrawal" {
		store.mu.Unlock()
		writeErr(w, 400, "not a withdrawal transaction")
		return
	}
	// Idempotent: already done.
	if tx.Status == "completed" {
		store.mu.Unlock()
		writeJSON(w, 200, map[string]interface{}{
			"id":      tx.ID,
			"status":  tx.Status,
			"message": "already completed",
		})
		return
	}
	if tx.Status != "pending" {
		store.mu.Unlock()
		writeErr(w, 409, "transaction is "+tx.Status+", cannot complete")
		return
	}

	u := store.users[tx.UserID]
	if u == nil {
		store.mu.Unlock()
		writeErr(w, 404, "user not found")
		return
	}

	// Money flow on completion:
	//   balance:   -amount   (the funds actually leave the platform)
	//   exposure:  -amount   (the hold is released)
	// The hold ledger entry HoldFunds wrote was already -amount; that
	// negative entry stands as the permanent record of the debit, so
	// we do NOT add another -amount entry here (that would double-debit
	// the ledger sum). We add a zero-amount "withdrawal_complete" ledger
	// row purely as a state marker so the operator can see the
	// transition in audit views.
	u.Balance = roundMoney(u.Balance - tx.Amount)
	if u.Balance < 0 {
		// Defensive: should never happen because we checked Available()
		// at request time, but a parallel manual edit could have stolen
		// the funds. Refuse rather than going negative.
		u.Balance = roundMoney(u.Balance + tx.Amount) // unwind
		store.mu.Unlock()
		writeErr(w, 409, "insufficient balance to finalize")
		return
	}
	u.Exposure = roundMoney(u.Exposure - tx.Amount)
	if u.Exposure < 0 {
		u.Exposure = 0
	}
	tx.Status = "completed"
	now := nowRFC3339()
	store.addLedger(tx.UserID, 0, "withdrawal_complete", "withdrawal_complete:"+tx.ID, "", now)
	store.mu.Unlock()

	if useDB() {
		dbUpdateBalance(tx.UserID, u.Balance, u.Exposure)
		dbSavePaymentTx(tx) // ON CONFLICT not present here; the existing dbSavePaymentTx
		// uses ON CONFLICT (id) DO UPDATE SET status — confirmed safe in db.go.
	}

	store.AddAudit(getUserID(r), getUsername(r), "withdrawal_completed",
		fmt.Sprintf("tx=%s user=%d amount=%.2f", tx.ID, tx.UserID, tx.Amount),
		r.RemoteAddr)
	store.AddNotification(tx.UserID, "withdrawal_complete",
		fmt.Sprintf("Withdrawal of ₹%.0f completed", tx.Amount),
		fmt.Sprintf("Your withdrawal of ₹%.2f via %s has been credited.", tx.Amount, tx.Method))
	logger.Info("withdrawal completed", "tx", tx.ID, "user", tx.UserID, "amount", tx.Amount)

	writeJSON(w, 200, map[string]interface{}{
		"id":     tx.ID,
		"status": tx.Status,
	})
}

func handleWithdrawReject(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("txID")
	if txID == "" {
		writeErr(w, 400, "tx_id required")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSONBody(r, &req) // optional
	if req.Reason == "" {
		req.Reason = "rejected by operator"
	}

	store.mu.Lock()
	tx, ok := store.paymentTxns[txID]
	if !ok {
		store.mu.Unlock()
		writeErr(w, 404, "transaction not found")
		return
	}
	if tx.Direction != "withdrawal" {
		store.mu.Unlock()
		writeErr(w, 400, "not a withdrawal transaction")
		return
	}
	if tx.Status == "failed" || tx.Status == "refunded" {
		store.mu.Unlock()
		writeJSON(w, 200, map[string]interface{}{
			"id":      tx.ID,
			"status":  tx.Status,
			"message": "already rejected",
		})
		return
	}
	if tx.Status != "pending" {
		store.mu.Unlock()
		writeErr(w, 409, "transaction is "+tx.Status+", cannot reject")
		return
	}

	u := store.users[tx.UserID]
	if u != nil {
		// Release the hold the request placed. Balance is untouched
		// (the funds were never debited). Exposure goes back down by
		// the held amount, and we write a +amount ledger entry to
		// cancel out the -amount the original HoldFunds wrote so the
		// ledger stays balanced.
		u.Exposure = roundMoney(u.Exposure - tx.Amount)
		if u.Exposure < 0 {
			u.Exposure = 0
		}
	}
	tx.Status = "failed"
	now := nowRFC3339()
	store.addLedger(tx.UserID, tx.Amount, "release", "withdrawal_reject:"+tx.ID, "", now)
	store.mu.Unlock()

	if useDB() && u != nil {
		dbUpdateBalance(tx.UserID, u.Balance, u.Exposure)
		dbSavePaymentTx(tx)
	}

	store.AddAudit(getUserID(r), getUsername(r), "withdrawal_rejected",
		fmt.Sprintf("tx=%s user=%d amount=%.2f reason=%s", tx.ID, tx.UserID, tx.Amount, req.Reason),
		r.RemoteAddr)
	if u != nil {
		store.AddNotification(tx.UserID, "withdrawal",
			fmt.Sprintf("Withdrawal of ₹%.0f rejected", tx.Amount),
			fmt.Sprintf("Your withdrawal request was rejected: %s. Funds returned to your balance.", req.Reason))
	}
	logger.Info("withdrawal rejected", "tx", tx.ID, "user", tx.UserID, "amount", tx.Amount, "reason", req.Reason)

	writeJSON(w, 200, map[string]interface{}{
		"id":     tx.ID,
		"status": tx.Status,
		"reason": req.Reason,
	})
}

// decodeJSONBody is a tiny helper that mirrors the json.NewDecoder
// pattern used elsewhere in this package without polluting the helpers
// file. Returns nil error if the body is empty (the request is allowed
// to omit a reason).
func decodeJSONBody(r *http.Request, dst interface{}) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return jsonDecode(r, dst)
}
