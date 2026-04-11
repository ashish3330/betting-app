package main

// Ledger reconciliation. Computes Σ(deposits) - Σ(withdrawals) +
// Σ(settlement) - Σ(commission) - Σ(hold) + Σ(release) for every user
// and compares it against (balance + exposure) on auth.users. The
// invariant a real betting exchange must satisfy is:
//
//     balance + exposure  ==  Σ(ledger.amount for that user)
//
// Any drift means we created or destroyed money somewhere — settlement
// missed an entry, exposure release used the wrong formula, an admin
// edited a balance directly, etc. This endpoint surfaces drift to the
// admin dashboard so it's actionable instead of silent.
//
// The check is read-only and runs in O(rows) time. For production with
// millions of ledger rows, schedule it as a nightly batch and store the
// last result; the live endpoint reads the cached result instead of
// re-scanning.

import (
	"net/http"
	"time"
)

type reconcileRow struct {
	UserID         int64   `json:"user_id"`
	Username       string  `json:"username"`
	Balance        float64 `json:"balance"`
	Exposure       float64 `json:"exposure"`
	LedgerSum      float64 `json:"ledger_sum"`
	Drift          float64 `json:"drift"`
	OK             bool    `json:"ok"`
}

type reconcileSummary struct {
	GeneratedAt    string         `json:"generated_at"`
	UsersChecked   int            `json:"users_checked"`
	UsersWithDrift int            `json:"users_with_drift"`
	TotalDrift     float64        `json:"total_drift"`
	Tolerance      float64        `json:"tolerance"`
	Rows           []reconcileRow `json:"rows"`
}

// handleReconcile is admin-only. Walks every user, sums their ledger,
// compares against (balance + exposure), and reports drift > tolerance.
//
// Tolerance defaults to 0.01 (one paisa) to absorb the well-known
// float64 rounding noise — see the audit doc on float→int migration.
func handleReconcile(w http.ResponseWriter, r *http.Request) {
	tolerance := 0.01

	if !useDB() {
		writeErr(w, 503, "reconciliation requires DB persistence")
		return
	}

	// Snapshot users from in-memory cache so we don't hold a lock during
	// per-row DB queries.
	store.mu.RLock()
	users := make([]*User, 0, len(store.users))
	for _, u := range store.users {
		users = append(users, u)
	}
	store.mu.RUnlock()

	out := reconcileSummary{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Tolerance:   tolerance,
		Rows:        make([]reconcileRow, 0, len(users)),
	}

	for _, u := range users {
		var ledgerSum float64
		err := db.QueryRow(
			`SELECT COALESCE(SUM(amount), 0) FROM betting.ledger WHERE user_id=$1`,
			u.ID,
		).Scan(&ledgerSum)
		if err != nil {
			logger.Error("reconcile: ledger sum failed", "user_id", u.ID, "error", err)
			continue
		}

		expected := roundMoney(ledgerSum)
		actual := roundMoney(u.Balance + u.Exposure)
		drift := roundMoney(actual - expected)

		row := reconcileRow{
			UserID:    u.ID,
			Username:  u.Username,
			Balance:   u.Balance,
			Exposure:  u.Exposure,
			LedgerSum: expected,
			Drift:     drift,
			OK:        absFloat(drift) <= tolerance,
		}
		out.Rows = append(out.Rows, row)
		out.UsersChecked++
		if !row.OK {
			out.UsersWithDrift++
			out.TotalDrift += drift
		}
	}

	// Audit so we know who ran reconciliation. Required for any
	// regulator review.
	store.AddAudit(getUserID(r), getUsername(r), "reconcile_run",
		"users="+itoa(out.UsersChecked)+" drift_count="+itoa(out.UsersWithDrift),
		r.RemoteAddr)

	writeJSON(w, 200, out)
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func itoa(i int) string {
	// avoid pulling in strconv at the package level when this file
	// only needs the single conversion.
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
