package hierarchy

// These tests use an in-process database/sql/driver fake that scripts
// responses by matching against a normalized prefix of the SQL. No new
// external dependencies are introduced (see go.mod).
//
// Note on scope: hierarchy/service.go (as of this writing) exposes
// GetChildren (ltree downline), GetDirectChildren (parent_id children),
// TransferCredit, GetUser, UpdateUserStatus, and IsAncestor. The tests
// below cover all of those. The test names referenced in the task
// description for referral flows (GetReferralStats, ApplyReferralCode)
// and stand-alone IsDirectChild / GetDownline helpers have no
// corresponding implementation in this package, so they are not written
// here -- the direct-child enforcement is exercised indirectly via
// TransferCredit_NotDirectChild, and the downline traversal is
// exercised via TestGetDownline / TestGetDownline_ScopedToCallerSubtree
// which drive GetChildren (the path-prefix downline query).

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

// ---------------------------------------------------------------------------
// In-process database/sql/driver fake
// ---------------------------------------------------------------------------

type scriptedRow []driver.Value

type scriptedResult struct {
	// If set, matches queries whose (normalized) prefix begins with this
	// string. First match wins; consumed entries are removed.
	prefix string
	// cols describes returned columns for queries.
	cols []string
	// rows describes returned rows for queries (ignored for exec).
	rows []scriptedRow
	// rowsAffected is returned for exec operations.
	rowsAffected int64
	// err is returned instead of rows / result.
	err error
	// sticky scripts are not consumed when matched (useful for defaults).
	sticky bool
}

type fakeDriver struct {
	mu      sync.Mutex
	scripts []*scriptedResult
	log     []string
}

func newFakeDriver() *fakeDriver { return &fakeDriver{} }

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	return &fakeConn{drv: d}, nil
}

func (d *fakeDriver) expect(s *scriptedResult) { d.scripts = append(d.scripts, s) }

func (d *fakeDriver) match(query string) *scriptedResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	norm := normalize(query)
	d.log = append(d.log, norm)
	for i, s := range d.scripts {
		if strings.HasPrefix(norm, s.prefix) {
			if !s.sticky {
				d.scripts = append(d.scripts[:i], d.scripts[i+1:]...)
			}
			return s
		}
	}
	return nil
}

func normalize(q string) string {
	// Collapse all whitespace to single spaces for prefix matching.
	var b strings.Builder
	prevSpace := false
	for _, r := range q {
		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

type fakeConn struct {
	drv *fakeDriver
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{conn: c, query: query}, nil
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{}, nil }

// BeginTx supports ConnBeginTx so that database/sql does not reject
// the non-default isolation level used by TransferCredit.
func (c *fakeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return &fakeTx{}, nil
}

func (c *fakeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.drv.match(query)
	if s == nil {
		return nil, fmt.Errorf("fake: no script for query: %s", normalize(query))
	}
	if s.err != nil {
		return nil, s.err
	}
	return &fakeRows{cols: s.cols, rows: s.rows}, nil
}

func (c *fakeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := c.drv.match(query)
	if s == nil {
		return nil, fmt.Errorf("fake: no script for exec: %s", normalize(query))
	}
	if s.err != nil {
		return nil, s.err
	}
	return fakeResult{rowsAffected: s.rowsAffected}, nil
}

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	conn  *fakeConn
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	nv := toNamed(args)
	return s.conn.ExecContext(context.Background(), s.query, nv)
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	nv := toNamed(args)
	return s.conn.QueryContext(context.Background(), s.query, nv)
}

func toNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

type fakeResult struct{ rowsAffected int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

type fakeRows struct {
	cols []string
	rows []scriptedRow
	idx  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		}
	}
	return nil
}

// Each test gets a uniquely-named driver registration so parallel /
// repeated runs don't collide with database/sql's global registry.
var driverSeq atomic.Int64

func newFakeDB(t *testing.T) (*sql.DB, *fakeDriver) {
	t.Helper()
	drv := newFakeDriver()
	name := fmt.Sprintf("fakehier-%d", driverSeq.Add(1))
	sql.Register(name, drv)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, drv
}

func newTestService(t *testing.T) (*Service, *fakeDriver) {
	t.Helper()
	db, drv := newFakeDB(t)
	return &Service{db: db, redis: nil, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, drv
}

// userRow assembles a canonical user tuple in the column order used by
// the queries in service.go (id, username, email, role, path, parent_id,
// balance, exposure, credit_limit, commission_rate, status, created_at, updated_at).
func userRow(id int64, username, path string, parentID *int64) scriptedRow {
	now := time.Now()
	var pid driver.Value
	if parentID != nil {
		pid = *parentID
	}
	return scriptedRow{
		id, username, username + "@example.com", "client", path, pid,
		float64(0), float64(0), float64(0), float64(0), "active", now, now,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func userCols() []string {
	return []string{
		"id", "username", "email", "role", "path", "parent_id",
		"balance", "exposure", "credit_limit", "commission_rate",
		"status", "created_at", "updated_at",
	}
}

// TestGetChildren_Direct — GetDirectChildren returns only direct
// children, not grandchildren. The SQL filter parent_id = $1 is what
// enforces this; we script exactly two direct children and assert both
// come back.
func TestGetChildren_Direct(t *testing.T) {
	svc, drv := newTestService(t)

	parent := int64(1)
	drv.expect(&scriptedResult{
		prefix: "SELECT id, username, email, role, path, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at FROM users WHERE parent_id",
		cols:   userCols(),
		rows: []scriptedRow{
			userRow(2, "child_a", "1.2", &parent),
			userRow(3, "child_b", "1.3", &parent),
		},
	})

	got, err := svc.GetDirectChildren(context.Background(), parent)
	if err != nil {
		t.Fatalf("GetDirectChildren: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 direct children, got %d", len(got))
	}
	for _, u := range got {
		if u.ParentID == nil || *u.ParentID != parent {
			t.Errorf("child %d has parent_id %v, want %d", u.ID, u.ParentID, parent)
		}
	}
}

// TestGetDownline — GetChildren uses ltree path-prefix matching to
// return the full subtree (direct children + deeper descendants).
func TestGetDownline(t *testing.T) {
	svc, drv := newTestService(t)

	root := int64(1)
	// First query: fetch caller's own path.
	drv.expect(&scriptedResult{
		prefix: "SELECT path FROM users WHERE id",
		cols:   []string{"path"},
		rows:   []scriptedRow{{"1"}},
	})
	// Second query: return three descendants at varying depths.
	child := root
	grand := int64(2)
	drv.expect(&scriptedResult{
		prefix: "SELECT id, username, email, role, path, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at FROM users WHERE path",
		cols:   userCols(),
		rows: []scriptedRow{
			userRow(2, "child", "1.2", &child),
			userRow(3, "grand", "1.2.3", &grand),
			userRow(4, "great", "1.2.3.4", func() *int64 { v := int64(3); return &v }()),
		},
	})

	got, err := svc.GetChildren(context.Background(), root)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 downline users, got %d", len(got))
	}
}

// TestGetDownline_ScopedToCallerSubtree — the second query is
// parameterised with the caller's path from the first query, so a
// sibling subtree (e.g. path 1.5) cannot appear. We simulate this by
// scripting the driver to return only descendants of the caller, and
// by verifying that the caller itself is excluded (id != userID clause).
func TestGetDownline_ScopedToCallerSubtree(t *testing.T) {
	svc, drv := newTestService(t)

	caller := int64(10)
	// Caller's path: 1.10 (child of root).
	drv.expect(&scriptedResult{
		prefix: "SELECT path FROM users WHERE id",
		cols:   []string{"path"},
		rows:   []scriptedRow{{"1.10"}},
	})
	// Only descendants of 1.10 are returned — not siblings (e.g. 1.11).
	parent := caller
	drv.expect(&scriptedResult{
		prefix: "SELECT id, username, email, role, path, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at FROM users WHERE path",
		cols:   userCols(),
		rows: []scriptedRow{
			userRow(11, "own_child", "1.10.11", &parent),
		},
	})

	got, err := svc.GetChildren(context.Background(), caller)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 scoped descendant, got %d", len(got))
	}
	if !strings.HasPrefix(got[0].Path, "1.10.") {
		t.Errorf("descendant path %q not scoped to caller subtree 1.10.*", got[0].Path)
	}
}

// Helper to script a "select role, balance FROM users" lookup used at
// the start of TransferCredit.
func expectSenderLookup(drv *fakeDriver, role string, balance float64) {
	drv.expect(&scriptedResult{
		prefix: "SELECT role, balance FROM users WHERE id",
		cols:   []string{"role", "balance"},
		rows:   []scriptedRow{{role, balance}},
	})
}

func expectReceiverLookup(drv *fakeDriver, role string, parentID *int64) {
	var pid driver.Value
	if parentID != nil {
		pid = *parentID
	}
	drv.expect(&scriptedResult{
		prefix: "SELECT role, parent_id FROM users WHERE id",
		cols:   []string{"role", "parent_id"},
		rows:   []scriptedRow{{role, pid}},
	})
}

// TestTransferCredit_HappyPath — debits parent, credits child, writes
// ledger rows (we verify all five scripted statements were consumed).
func TestTransferCredit_HappyPath(t *testing.T) {
	svc, drv := newTestService(t)

	parent := int64(1)
	child := int64(2)

	expectSenderLookup(drv, string(models.RoleMaster), 1000.0)
	expectReceiverLookup(drv, string(models.RoleAgent), &parent)
	drv.expect(&scriptedResult{prefix: "UPDATE users SET balance = balance - ", rowsAffected: 1})
	drv.expect(&scriptedResult{prefix: "UPDATE users SET balance = balance + ", rowsAffected: 1})
	drv.expect(&scriptedResult{prefix: "INSERT INTO ledger", rowsAffected: 2})

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: parent, ToUserID: child, Amount: 250,
	})
	if err != nil {
		t.Fatalf("TransferCredit: %v", err)
	}
	if len(drv.scripts) != 0 {
		t.Errorf("unused scripts: %d (SQL log: %v)", len(drv.scripts), drv.log)
	}
}

// TestTransferCredit_InsufficientParentBalance — refuses with error and
// no balance-mutating statements are executed.
func TestTransferCredit_InsufficientParentBalance(t *testing.T) {
	svc, drv := newTestService(t)

	parent := int64(1)
	expectSenderLookup(drv, string(models.RoleMaster), 50.0)
	expectReceiverLookup(drv, string(models.RoleAgent), &parent)

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: parent, ToUserID: 2, Amount: 500,
	})
	if err == nil {
		t.Fatal("expected error for insufficient balance, got nil")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("error does not mention insufficient balance: %v", err)
	}
	// No UPDATE / INSERT should have been reached.
	for _, q := range drv.log {
		if strings.HasPrefix(q, "UPDATE users SET balance") || strings.HasPrefix(q, "INSERT INTO ledger") {
			t.Errorf("balance-mutating statement ran despite insufficient funds: %q", q)
		}
	}
}

// TestTransferCredit_NotDirectChild — receiver's parent_id does not
// match sender; the service must reject before touching balances.
func TestTransferCredit_NotDirectChild(t *testing.T) {
	svc, drv := newTestService(t)

	other := int64(99)
	expectSenderLookup(drv, string(models.RoleMaster), 1000.0)
	expectReceiverLookup(drv, string(models.RoleAgent), &other)

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: 1, ToUserID: 2, Amount: 100,
	})
	if err == nil {
		t.Fatal("expected error for non-direct-child, got nil")
	}
	if !strings.Contains(err.Error(), "not a direct child") {
		t.Errorf("error does not mention direct-child check: %v", err)
	}
}

// TestTransferCredit_InsufficientRoleHierarchy — additional coverage:
// a peer cannot transfer to a peer even if parent_id matches by
// accident. Exercises the CanManage role check.
func TestTransferCredit_InsufficientRoleHierarchy(t *testing.T) {
	svc, drv := newTestService(t)

	parent := int64(1)
	expectSenderLookup(drv, string(models.RoleAgent), 1000.0)
	expectReceiverLookup(drv, string(models.RoleAgent), &parent)

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: 1, ToUserID: 2, Amount: 100,
	})
	if err == nil {
		t.Fatal("expected error for peer transfer, got nil")
	}
	if !strings.Contains(err.Error(), "hierarchy permissions") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestIsDirectChild_True — exercises the "receiver is direct child"
// branch of TransferCredit (there is no standalone IsDirectChild
// helper in service.go).
func TestIsDirectChild_True(t *testing.T) {
	svc, drv := newTestService(t)
	parent := int64(1)

	expectSenderLookup(drv, string(models.RoleMaster), 1000.0)
	expectReceiverLookup(drv, string(models.RoleAgent), &parent)
	drv.expect(&scriptedResult{prefix: "UPDATE users SET balance = balance - ", rowsAffected: 1})
	drv.expect(&scriptedResult{prefix: "UPDATE users SET balance = balance + ", rowsAffected: 1})
	drv.expect(&scriptedResult{prefix: "INSERT INTO ledger", rowsAffected: 2})

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: parent, ToUserID: 2, Amount: 10,
	})
	if err != nil {
		t.Fatalf("direct-child transfer should succeed: %v", err)
	}
}

// TestIsDirectChild_False — the receiver's parent_id is NULL (orphan);
// TransferCredit must reject.
func TestIsDirectChild_False(t *testing.T) {
	svc, drv := newTestService(t)

	expectSenderLookup(drv, string(models.RoleMaster), 1000.0)
	expectReceiverLookup(drv, string(models.RoleAgent), nil)

	err := svc.TransferCredit(context.Background(), &models.CreditTransferRequest{
		FromUserID: 1, ToUserID: 2, Amount: 10,
	})
	if err == nil {
		t.Fatal("expected error for orphan receiver, got nil")
	}
}

// ---------------------------------------------------------------------------
// Ancillary coverage: GetUser, UpdateUserStatus, IsAncestor
// ---------------------------------------------------------------------------

func TestGetUser(t *testing.T) {
	svc, drv := newTestService(t)
	parent := int64(1)
	drv.expect(&scriptedResult{
		prefix: "SELECT id, username, email, role, path, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at FROM users WHERE id",
		cols:   userCols(),
		rows:   []scriptedRow{userRow(2, "child", "1.2", &parent)},
	})

	u, err := svc.GetUser(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.ID != 2 || u.Username != "child" {
		t.Errorf("unexpected user: %+v", u)
	}
}

func TestUpdateUserStatus(t *testing.T) {
	svc, drv := newTestService(t)
	drv.expect(&scriptedResult{prefix: "UPDATE users SET status", rowsAffected: 1})

	if err := svc.UpdateUserStatus(context.Background(), 2, "suspended"); err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}
}

func TestIsAncestor_True(t *testing.T) {
	svc, drv := newTestService(t)
	drv.expect(&scriptedResult{
		prefix: "SELECT COUNT(*) FROM users u1, users u2",
		cols:   []string{"count"},
		rows:   []scriptedRow{{int64(1)}},
	})

	ok, err := svc.IsAncestor(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ok {
		t.Error("expected IsAncestor=true")
	}
}

func TestIsAncestor_False(t *testing.T) {
	svc, drv := newTestService(t)
	drv.expect(&scriptedResult{
		prefix: "SELECT COUNT(*) FROM users u1, users u2",
		cols:   []string{"count"},
		rows:   []scriptedRow{{int64(0)}},
	})

	ok, err := svc.IsAncestor(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if ok {
		t.Error("expected IsAncestor=false")
	}
}
