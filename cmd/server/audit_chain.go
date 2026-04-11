package main

// Tamper-evident audit log via SHA-256 hash chain.
//
// Each new audit row stores the SHA-256 hash of (previous_row_hash || canonical_row_data).
// To detect tampering: walk the chain in id order and recompute each hash;
// any mismatch indicates a row was edited or deleted after the fact.
//
// This is the cheapest possible append-only guarantee that doesn't require
// an external service. A real licensed operator would also archive the
// chain head (the latest hash) to an immutable external store every N
// minutes — that turns local DB tampering into a detectable mismatch
// against the externally archived hash.
//
// Storage: a new hash_chain TEXT column on betting.audit_log added by
// runMigrations. Backfilled with empty string for legacy rows; the chain
// is only enforced for rows written after the column exists.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
)

// chainState tracks the latest hash so we don't have to round-trip to
// the DB on every audit insert. The mutex serializes audit chain
// writes — they're already low-volume (~5 per request) and serialization
// is required to keep the chain linear.
var (
	chainMu       sync.Mutex
	lastChainHash string // hex-encoded sha256, or "" if uninitialized
)

// initAuditChain loads the latest hash from the DB at startup so the
// chain continues across restarts.
func initAuditChain() {
	if !useDB() {
		return
	}
	var h string
	err := db.QueryRow(`SELECT COALESCE(hash_chain, '') FROM betting.audit_log WHERE hash_chain != '' ORDER BY id DESC LIMIT 1`).Scan(&h)
	if err != nil {
		// Empty table or column missing — chain starts fresh on next write.
		return
	}
	chainMu.Lock()
	lastChainHash = h
	chainMu.Unlock()
}

// computeChainHash takes the canonical representation of an audit row
// and links it to the previous hash. Result is stored alongside the row.
func computeChainHash(prev string, userID int64, action, details, ip string) string {
	canonical := fmt.Sprintf("%s|%d|%s|%s|%s", prev, userID, action, details, ip)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// nextChainHash advances the chain by one entry and returns the new
// hash to persist. Must be called under chainMu.
func nextChainHashLocked(userID int64, action, details, ip string) string {
	h := computeChainHash(lastChainHash, userID, action, details, ip)
	lastChainHash = h
	return h
}

// AuditChainNext is the public entry point used by dbAddAudit.
// Returns the hash to write into the new row's hash_chain column.
func AuditChainNext(userID int64, action, details, ip string) string {
	chainMu.Lock()
	defer chainMu.Unlock()
	return nextChainHashLocked(userID, action, details, ip)
}

// handleVerifyAuditChain re-walks every chained row and reports the
// first id where the recomputed hash diverges from the stored hash.
// Admin-only. Read-only.
func handleVerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	if !useDB() {
		writeErr(w, 503, "audit chain verification requires DB persistence")
		return
	}

	rows, err := db.Query(`
		SELECT id, COALESCE(user_id, 0), COALESCE(action, ''), COALESCE(details, ''), COALESCE(ip, ''), COALESCE(hash_chain, '')
		FROM betting.audit_log
		WHERE hash_chain != ''
		ORDER BY id ASC
	`)
	if err != nil {
		writeErr(w, 500, "verify failed: "+err.Error())
		return
	}
	defer rows.Close()

	var prev string
	checked := 0
	var firstBadID int64
	var firstBadStored string
	var firstBadComputed string
	for rows.Next() {
		var id int64
		var userID int64
		var action, details, ip, stored string
		if err := rows.Scan(&id, &userID, &action, &details, &ip, &stored); err != nil {
			continue
		}
		computed := computeChainHash(prev, userID, action, details, ip)
		checked++
		if computed != stored {
			if firstBadID == 0 {
				firstBadID = id
				firstBadStored = stored
				firstBadComputed = computed
			}
		}
		// Use stored value to continue the walk so a single tamper doesn't
		// cascade — every subsequent row is independently verifiable
		// against its declared predecessor.
		prev = stored
	}

	out := map[string]interface{}{
		"checked":   checked,
		"ok":        firstBadID == 0,
		"chain_head": lastChainHashSnapshot(),
	}
	if firstBadID != 0 {
		out["first_tamper_id"] = firstBadID
		out["stored_hash"] = firstBadStored
		out["computed_hash"] = firstBadComputed
	}

	store.AddAudit(getUserID(r), getUsername(r), "audit_chain_verified",
		fmt.Sprintf("checked=%d ok=%v", checked, firstBadID == 0), r.RemoteAddr)

	writeJSON(w, 200, out)
}

func lastChainHashSnapshot() string {
	chainMu.Lock()
	defer chainMu.Unlock()
	return lastChainHash
}
