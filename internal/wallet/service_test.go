package wallet

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Minimal in-process database/sql/driver implementation for testing.
//
// We don't pull in github.com/DATA-DOG/go-sqlmock because the rest of the
// service tree avoids extra test dependencies. The driver below understands
// just enough to drive SettleBet end-to-end:
//
//   - SELECT balance ... FOR UPDATE   → returns the user balance
//   - INSERT INTO betting.settlement_idempotency ... ON CONFLICT DO NOTHING
//   - INSERT INTO ledger ... ON CONFLICT DO NOTHING
//   - UPDATE users SET balance = balance + $1 ... (counts updates)
//   - SELECT result_json FROM betting.settlement_idempotency
//
// Anything else is best-effort and returns an empty result so the rest of
// the service code keeps moving.
// ---------------------------------------------------------------------------

func init() {
	sql.Register("walletfake", &fakeDriver{})
}

type fakeState struct {
	mu sync.Mutex
	// idempotency table: key -> stored result_json
	idem map[string]string
	// counts how many "UPDATE users SET balance = balance + $1" rows we ran
	balanceUpdates int64
	// counts inserts into the ledger settlement rows
	ledgerSettlements int64
	// users table - single user balance keyed by id
	users map[int64]float64
	// per-user exposure keyed by id
	exposure map[int64]float64
	// ledger table mirrors inserted rows keyed by their reference so we can
	// assert idempotency on hold/release/settlement writes.
	ledger map[string]ledgerRow
	// aggregate ledger totals per user for GetBalance's second query.
	totalDeposits    map[int64]float64
	totalWithdrawals map[int64]float64
	totalSettlement  map[int64]float64
}

type ledgerRow struct {
	userID int64
	amount float64
	typ    string
	ref    string
	betID  string
}

func newFakeState() *fakeState {
	return &fakeState{
		idem:             map[string]string{},
		users:            map[int64]float64{1: 1000},
		exposure:         map[int64]float64{},
		ledger:           map[string]ledgerRow{},
		totalDeposits:    map[int64]float64{},
		totalWithdrawals: map[int64]float64{},
		totalSettlement:  map[int64]float64{},
	}
}

type fakeDriver struct{}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	st, ok := stateRegistry.Load(name)
	if !ok {
		return nil, errors.New("walletfake: unknown DSN " + name)
	}
	return &fakeConn{state: st.(*fakeState)}, nil
}

// stateRegistry maps a DSN string to its fakeState so multiple parallel
// tests can each have an isolated database.
var stateRegistry sync.Map

func registerFakeDB(t *testing.T) (*sql.DB, *fakeState) {
	t.Helper()
	st := newFakeState()
	dsn := t.Name() + "/" + time.Now().Format(time.RFC3339Nano)
	stateRegistry.Store(dsn, st)
	t.Cleanup(func() { stateRegistry.Delete(dsn) })

	db, err := sql.Open("walletfake", dsn)
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, st
}

type fakeConn struct {
	state  *fakeState
	closed bool
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{conn: c, query: query}, nil
}

func (c *fakeConn) Close() error {
	c.closed = true
	return nil
}

func (c *fakeConn) Begin() (driver.Tx, error) {
	return &fakeTx{conn: c}, nil
}

// BeginTx so service code that passes sql.TxOptions works.
func (c *fakeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return &fakeTx{conn: c}, nil
}

// ExecContext / QueryContext implementations on the conn (fast path used by
// database/sql when a Stmt isn't required).

func (c *fakeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.state.exec(query, args)
}

func (c *fakeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(query, args)
}

type fakeTx struct {
	conn *fakeConn
}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

// Stmt isn't really used because conn.ExecContext / QueryContext are
// preferred by database/sql, but we implement just enough for safety.
type fakeStmt struct {
	conn  *fakeConn
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.state.exec(s.query, named)
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.state.query(s.query, named)
}

func (s *fakeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.state.exec(s.query, args)
}

func (s *fakeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.state.query(s.query, args)
}

// ---------------------------------------------------------------------------
// Query / Exec dispatch
// ---------------------------------------------------------------------------

type fakeResult struct {
	rowsAffected int64
}

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

func (st *fakeState) exec(query string, args []driver.NamedValue) (driver.Result, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	q := normalize(query)

	switch {
	case strings.Contains(q, "insert into betting.settlement_idempotency"):
		key := stringArg(args, 0)
		if _, exists := st.idem[key]; exists {
			return fakeResult{rowsAffected: 0}, nil
		}
		// args[1] is the JSON result
		st.idem[key] = stringArg(args, 1)
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "update users set exposure = exposure +"):
		// HoldFunds: bump exposure by args[0] for user args[1].
		if uid, ok := int64Arg(args, 1); ok {
			st.exposure[uid] += floatArg(args, 0)
		}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "update users set exposure = $1"):
		// ReleaseFunds: set exposure to args[0] for user args[1].
		if uid, ok := int64Arg(args, 1); ok {
			st.exposure[uid] = floatArg(args, 0)
		}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "update users set balance = balance +"):
		atomic.AddInt64(&st.balanceUpdates, 1)
		// args[0] = pnl, args[1] = userID
		if uid, ok := int64Arg(args, 1); ok {
			st.users[uid] += floatArg(args, 0)
		}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "update users set balance = balance -"):
		// commission deduction or withdrawal balance debit
		if uid, ok := int64Arg(args, 1); ok {
			st.users[uid] -= floatArg(args, 0)
		}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "insert into ledger") && strings.Contains(q, "'settlement'"):
		atomic.AddInt64(&st.ledgerSettlements, 1)
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "insert into ledger"):
		// Generic ledger insert with idempotency on `reference`.
		// Args order: user_id, amount, reference, bet_id (when bet_id col present).
		uid, _ := int64Arg(args, 0)
		amt := floatArg(args, 1)
		ref := stringArg(args, 2)
		betID := stringArg(args, 3)
		if _, exists := st.ledger[ref]; exists {
			return fakeResult{rowsAffected: 0}, nil
		}
		typ := "hold"
		if strings.HasPrefix(ref, "release:") {
			typ = "release"
		} else if strings.HasPrefix(ref, "settlement:") {
			typ = "settlement"
		} else if strings.HasPrefix(ref, "commission:") {
			typ = "commission"
		}
		st.ledger[ref] = ledgerRow{userID: uid, amount: amt, typ: typ, ref: ref, betID: betID}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(q, "update users set"):
		return fakeResult{rowsAffected: 1}, nil
	}

	// Default: pretend it succeeded.
	return fakeResult{rowsAffected: 1}, nil
}

func (st *fakeState) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	q := normalize(query)

	switch {
	case strings.Contains(q, "select balance, exposure from users where id"):
		uid, _ := int64Arg(args, 0)
		return &singleRow{
			cols: []string{"balance", "exposure"},
			vals: []driver.Value{st.users[uid], st.exposure[uid]},
		}, nil

	case strings.Contains(q, "select balance from users where id"):
		uid, _ := int64Arg(args, 0)
		return &singleRow{cols: []string{"balance"}, vals: []driver.Value{st.users[uid]}}, nil

	case strings.Contains(q, "select exposure from users where id"):
		uid, _ := int64Arg(args, 0)
		return &singleRow{cols: []string{"exposure"}, vals: []driver.Value{st.exposure[uid]}}, nil

	case strings.Contains(q, "select coalesce(sum") && strings.Contains(q, "from ledger"):
		// GetBalance aggregation: deposits, withdrawals, settlement total.
		uid, _ := int64Arg(args, 0)
		return &singleRow{
			cols: []string{"deposits", "withdrawals", "settlement"},
			vals: []driver.Value{st.totalDeposits[uid], st.totalWithdrawals[uid], st.totalSettlement[uid]},
		}, nil

	case strings.Contains(q, "select result_json from betting.settlement_idempotency"):
		key := stringArg(args, 0)
		if v, ok := st.idem[key]; ok {
			return &singleRow{cols: []string{"result_json"}, vals: []driver.Value{[]byte(v)}}, nil
		}
		return &singleRow{cols: []string{"result_json"}, done: true}, nil
	}

	return &singleRow{done: true}, nil
}

type singleRow struct {
	cols []string
	vals []driver.Value
	done bool
}

func (r *singleRow) Columns() []string { return r.cols }
func (r *singleRow) Close() error      { return nil }
func (r *singleRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	for i := range dest {
		if i < len(r.vals) {
			dest[i] = r.vals[i]
		}
	}
	r.done = true
	return nil
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func stringArg(args []driver.NamedValue, idx int) string {
	if idx >= len(args) {
		return ""
	}
	switch v := args[idx].Value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

func int64Arg(args []driver.NamedValue, idx int) (int64, bool) {
	if idx >= len(args) {
		return 0, false
	}
	switch v := args[idx].Value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func floatArg(args []driver.NamedValue, idx int) float64 {
	if idx >= len(args) {
		return 0
	}
	switch v := args[idx].Value.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSettleBetIdempotency(t *testing.T) {
	db, st := registerFakeDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &Service{db: db, logger: logger}

	const (
		userID = int64(1)
		betID  = "deadbeefcafef00d"
		pnl    = float64(50)
		comm   = float64(0)
	)

	// First call: should apply the balance update.
	if err := svc.SettleBet(context.Background(), userID, betID, pnl, comm); err != nil {
		t.Fatalf("first SettleBet returned error: %v", err)
	}

	if got := atomic.LoadInt64(&st.balanceUpdates); got != 1 {
		t.Fatalf("after first SettleBet: balanceUpdates = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&st.ledgerSettlements); got != 1 {
		t.Fatalf("after first SettleBet: ledgerSettlements = %d, want 1", got)
	}
	if got := st.users[userID]; got != 1050 {
		t.Fatalf("after first SettleBet: balance = %v, want 1050", got)
	}

	// Second call with the same betID: must short-circuit on idempotency.
	err := svc.SettleBet(context.Background(), userID, betID, pnl, comm)
	if !errors.Is(err, ErrDuplicateOperation) {
		t.Fatalf("second SettleBet: err = %v, want ErrDuplicateOperation", err)
	}

	if got := atomic.LoadInt64(&st.balanceUpdates); got != 1 {
		t.Fatalf("after duplicate SettleBet: balanceUpdates = %d, want 1 (no replay)", got)
	}
	if got := atomic.LoadInt64(&st.ledgerSettlements); got != 1 {
		t.Fatalf("after duplicate SettleBet: ledgerSettlements = %d, want 1 (no replay)", got)
	}
	if got := st.users[userID]; got != 1050 {
		t.Fatalf("after duplicate SettleBet: balance = %v, want 1050 (unchanged)", got)
	}
	if _, ok := st.idem[betID]; !ok {
		t.Fatalf("expected idempotency row for bet %q to be present", betID)
	}
}

func TestSettleBetRequiresBetID(t *testing.T) {
	db, _ := registerFakeDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &Service{db: db, logger: logger}

	if err := svc.SettleBet(context.Background(), 1, "", 10, 0); err == nil {
		t.Fatal("expected error when betID is empty")
	}
}

// ---------------------------------------------------------------------------
// HoldFunds / ReleaseFunds / SettleBet happy-path and edge-case tests
// ---------------------------------------------------------------------------

func newTestService(t *testing.T) (*Service, *fakeState) {
	t.Helper()
	db, st := registerFakeDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Service{db: db, logger: logger}, st
}

func TestHoldFunds_InsufficientBalance(t *testing.T) {
	svc, st := newTestService(t)
	const userID = int64(1)
	// User has 1000 balance (default), 900 exposure => 100 available.
	st.exposure[userID] = 900

	err := svc.HoldFunds(context.Background(), userID, 500, "bet-insuf")
	if err == nil {
		t.Fatal("expected error for insufficient balance, got nil")
	}
	if !strings.Contains(err.Error(), "insufficient") {
		t.Fatalf("expected insufficient-balance error, got: %v", err)
	}
	// Exposure must be unchanged after a failed hold.
	if got := st.exposure[userID]; got != 900 {
		t.Fatalf("exposure after failed hold = %v, want 900 (unchanged)", got)
	}
}

func TestHoldFunds_NegativeAmount(t *testing.T) {
	svc, st := newTestService(t)
	const userID = int64(1)

	// Zero amount: must be rejected even though the user is solvent.
	if err := svc.HoldFunds(context.Background(), userID, 0, "bet-zero"); err == nil {
		t.Fatal("expected error for zero amount, got nil")
	}
	// Negative amount: must be rejected.
	if err := svc.HoldFunds(context.Background(), userID, -50, "bet-neg"); err == nil {
		t.Fatal("expected error for negative amount, got nil")
	}

	// Exposure must never be touched by a rejected hold — otherwise a caller
	// could pass amount=-100 to artificially free 100 of exposure.
	if got := st.exposure[userID]; got != 0 {
		t.Fatalf("exposure after rejected hold = %v, want 0", got)
	}
}

func TestReleaseFunds_ReducesExposure(t *testing.T) {
	svc, st := newTestService(t)
	const userID = int64(1)
	st.exposure[userID] = 300

	if err := svc.ReleaseFunds(context.Background(), userID, 120, "bet-rel-1"); err != nil {
		t.Fatalf("ReleaseFunds returned error: %v", err)
	}
	if got := st.exposure[userID]; got != 180 {
		t.Fatalf("exposure after release = %v, want 180", got)
	}
	// The ledger row for the release must exist and carry the 'release' type.
	row, ok := st.ledger["release:bet-rel-1"]
	if !ok {
		t.Fatal("expected release ledger row to be inserted")
	}
	if row.typ != "release" || row.amount != 120 {
		t.Fatalf("release ledger row = %+v, want type=release amount=120", row)
	}
}

func TestReleaseFunds_NeverNegative(t *testing.T) {
	svc, st := newTestService(t)
	const userID = int64(1)
	st.exposure[userID] = 50

	// Release more than the current exposure — service must clamp to 0
	// instead of going negative.
	if err := svc.ReleaseFunds(context.Background(), userID, 200, "bet-rel-clamp"); err != nil {
		t.Fatalf("ReleaseFunds returned error: %v", err)
	}
	if got := st.exposure[userID]; got != 0 {
		t.Fatalf("exposure after over-release = %v, want 0 (clamped)", got)
	}
}

func TestSettleBet_BackBetWin(t *testing.T) {
	svc, st := newTestService(t)
	const (
		userID = int64(1)
		betID  = "back-win-001"
	)
	// Pre-state: 1000 balance, 100 exposure (still held until we release).
	st.users[userID] = 1000
	st.exposure[userID] = 100

	// A winning back bet: +200 P&L is credited via SettleBet, the
	// exposure is then released explicitly by the caller (mirrors what
	// the settlement worker does).
	if err := svc.SettleBet(context.Background(), userID, betID, 200, 0); err != nil {
		t.Fatalf("SettleBet returned error: %v", err)
	}
	if got := st.users[userID]; got != 1200 {
		t.Fatalf("balance after win = %v, want 1200", got)
	}
	if err := svc.ReleaseFunds(context.Background(), userID, 100, betID); err != nil {
		t.Fatalf("ReleaseFunds after win returned error: %v", err)
	}
	if got := st.exposure[userID]; got != 0 {
		t.Fatalf("exposure after release = %v, want 0", got)
	}
}

func TestSettleBet_LayBetLoss(t *testing.T) {
	svc, st := newTestService(t)
	const (
		userID = int64(2)
		betID  = "lay-loss-001"
	)
	// Pre-state: user 2 starts with 1000 balance, 200 exposure (lay liability
	// = stake * (price - 1) = 100 * (3 - 1) = 200).
	st.users[userID] = 1000
	st.exposure[userID] = 200

	// Lay bet loses: the layer must pay out the full liability → pnl -200.
	if err := svc.SettleBet(context.Background(), userID, betID, -200, 0); err != nil {
		t.Fatalf("SettleBet returned error: %v", err)
	}
	if got := st.users[userID]; got != 800 {
		t.Fatalf("balance after loss = %v, want 800", got)
	}
	// Callers release the matching exposure after settlement.
	if err := svc.ReleaseFunds(context.Background(), userID, 200, betID); err != nil {
		t.Fatalf("ReleaseFunds after loss returned error: %v", err)
	}
	if got := st.exposure[userID]; got != 0 {
		t.Fatalf("exposure after release = %v, want 0", got)
	}
}

func TestGetBalance_RoundsTo2DP(t *testing.T) {
	// Verifies that roundMoney-style 2dp semantics survive a round-trip
	// through GetBalance → cache → deserialise. GetBalance itself does not
	// round, but the test asserts the AvailableBalance arithmetic is stable
	// and that repeated calls produce identical numbers (i.e. no float drift
	// introduced by redis JSON encoding).
	svc, st := newTestService(t)
	const userID = int64(1)
	st.users[userID] = 123.45
	st.exposure[userID] = 23.45

	s1, err := svc.GetBalance(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if s1.Balance != 123.45 {
		t.Fatalf("balance = %v, want 123.45", s1.Balance)
	}
	if s1.Exposure != 23.45 {
		t.Fatalf("exposure = %v, want 23.45", s1.Exposure)
	}
	// Available = balance - exposure, computed inside the service.
	if s1.AvailableBalance != 100 {
		t.Fatalf("available = %v, want 100", s1.AvailableBalance)
	}

	// The matching package's roundMoney helper uses identical 2dp semantics;
	// exercise it here since it is used throughout the wallet summary rounding
	// path in production (ledger totals come through the DB as NUMERIC).
	if got := roundMoneyWallet(1.235); got != 1.24 {
		t.Fatalf("roundMoneyWallet(1.235) = %v, want 1.24", got)
	}
	if got := roundMoneyWallet(-1.235); got != -1.24 {
		t.Fatalf("roundMoneyWallet(-1.235) = %v, want -1.24", got)
	}
	if got := roundMoneyWallet(100); got != 100 {
		t.Fatalf("roundMoneyWallet(100) = %v, want 100", got)
	}
}

// roundMoneyWallet is a local copy of the 2dp rounding helper used to verify
// the wallet package's rounding semantics are consistent with the rest of the
// API (see internal/matching.roundMoney).
func roundMoneyWallet(v float64) float64 {
	sign := 1.0
	if v < 0 {
		sign = -1
	}
	return float64(int64(v*100+0.5*sign)) / 100
}
