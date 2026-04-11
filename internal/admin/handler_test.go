package admin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

// ---------------------------------------------------------------------------
// Minimal in-process database/sql/driver fake, modelled on the one in
// internal/wallet/service_test.go. This fake understands just enough SQL to
// drive the admin panel handlers (and the seed bootstrap path) end-to-end
// without pulling in sqlmock or a real Postgres.
// ---------------------------------------------------------------------------

func init() {
	sql.Register("adminfake", &adminFakeDriver{})
}

type fakeUser struct {
	ID             int64
	Username       string
	Email          string
	Role           models.Role
	Path           string
	ParentID       *int64
	Balance        float64
	Exposure       float64
	CreditLimit    float64
	CommissionRate float64
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type fakeAudit struct {
	ID         int64
	ActorID    int64
	Action     string
	EntityType string
	EntityID   string
	IPAddress  string
	CreatedAt  time.Time
}

type fakeBet struct {
	ID        string
	MarketID  string
	UserID    int64
	Stake     float64
	Profit    float64
	Status    string
	Sport     string
	CreatedAt time.Time
}

type adminFakeState struct {
	mu sync.Mutex

	users       map[int64]*fakeUser
	usersByName map[string]int64
	nextUserID  int64

	audits []fakeAudit
	bets   []fakeBet

	// counters the seed tests use to verify effects
	registerInsertCalls int64
	depositCalls        int64
	balanceCredits      int64
	balanceDebits       int64
	sampleBetInserts    int64
}

func newAdminFakeState() *adminFakeState {
	return &adminFakeState{
		users:       map[int64]*fakeUser{},
		usersByName: map[string]int64{},
		nextUserID:  1,
	}
}

func (st *adminFakeState) addUser(u *fakeUser) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if u.ID == 0 {
		u.ID = st.nextUserID
		st.nextUserID++
	}
	if u.ID >= st.nextUserID {
		st.nextUserID = u.ID + 1
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = u.CreatedAt
	}
	if u.Status == "" {
		u.Status = "active"
	}
	st.users[u.ID] = u
	st.usersByName[u.Username] = u.ID
}

// adminFakeRegistry maps a DSN string to its adminFakeState so multiple
// parallel tests can each have an isolated database.
var adminFakeRegistry sync.Map

func registerAdminFakeDB(t *testing.T) (*sql.DB, *adminFakeState) {
	t.Helper()
	st := newAdminFakeState()
	dsn := "admin/" + t.Name() + "/" + time.Now().Format(time.RFC3339Nano)
	adminFakeRegistry.Store(dsn, st)
	t.Cleanup(func() { adminFakeRegistry.Delete(dsn) })

	db, err := sql.Open("adminfake", dsn)
	if err != nil {
		t.Fatalf("open adminfake db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, st
}

// ---------------------------------------------------------------------------
// Driver / Conn / Stmt plumbing.
// ---------------------------------------------------------------------------

type adminFakeDriver struct{}

func (d *adminFakeDriver) Open(name string) (driver.Conn, error) {
	v, ok := adminFakeRegistry.Load(name)
	if !ok {
		return nil, errors.New("adminfake: unknown DSN " + name)
	}
	return &adminFakeConn{state: v.(*adminFakeState)}, nil
}

type adminFakeConn struct {
	state  *adminFakeState
	closed bool
}

func (c *adminFakeConn) Prepare(query string) (driver.Stmt, error) {
	return &adminFakeStmt{conn: c, query: query}, nil
}

func (c *adminFakeConn) Close() error {
	c.closed = true
	return nil
}

func (c *adminFakeConn) Begin() (driver.Tx, error) {
	return &adminFakeTx{conn: c}, nil
}

func (c *adminFakeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return &adminFakeTx{conn: c}, nil
}

func (c *adminFakeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.state.exec(query, args)
}

func (c *adminFakeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(query, args)
}

type adminFakeTx struct {
	conn *adminFakeConn
}

func (t *adminFakeTx) Commit() error   { return nil }
func (t *adminFakeTx) Rollback() error { return nil }

type adminFakeStmt struct {
	conn  *adminFakeConn
	query string
}

func (s *adminFakeStmt) Close() error  { return nil }
func (s *adminFakeStmt) NumInput() int { return -1 }

func (s *adminFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.state.exec(s.query, toNamed(args))
}

func (s *adminFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.state.query(s.query, toNamed(args))
}

func (s *adminFakeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.state.exec(s.query, args)
}

func (s *adminFakeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.state.query(s.query, args)
}

func toNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// ---------------------------------------------------------------------------
// exec / query dispatch
// ---------------------------------------------------------------------------

type adminFakeResult struct{ rowsAffected int64 }

func (r adminFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r adminFakeResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

func (st *adminFakeState) exec(query string, args []driver.NamedValue) (driver.Result, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	q := normalizeSQL(query)

	switch {
	// --- auth.Service.Register path ---------------------------------------
	// The INSERT ... RETURNING path uses QueryContext, not ExecContext, so
	// it arrives via st.query. We only see follow-up statements here:
	//   UPDATE users SET path = $1 WHERE id = $2
	case strings.HasPrefix(q, "update users set path ="):
		path := strArg(args, 0)
		id, _ := intArg(args, 1)
		if u, ok := st.users[id]; ok {
			u.Path = path
		}
		return adminFakeResult{rowsAffected: 1}, nil

	// --- wallet.Service.Deposit path + seed.transferCredit fallback -------
	case strings.HasPrefix(q, "update users set balance = balance + $1, updated_at = now()"):
		atomic.AddInt64(&st.balanceCredits, 1)
		amt := floatArg(args, 0)
		id, _ := intArg(args, 1)
		if u, ok := st.users[id]; ok {
			u.Balance += amt
		}
		return adminFakeResult{rowsAffected: 1}, nil

	case strings.HasPrefix(q, "update users set balance = balance - $1, updated_at = now()"):
		atomic.AddInt64(&st.balanceDebits, 1)
		amt := floatArg(args, 0)
		id, _ := intArg(args, 1)
		if u, ok := st.users[id]; ok {
			u.Balance -= amt
		}
		return adminFakeResult{rowsAffected: 1}, nil

	// Seed's sample bet insert
	case strings.HasPrefix(q, "insert into bets ("):
		atomic.AddInt64(&st.sampleBetInserts, 1)
		id := strArg(args, 0)
		marketID := strArg(args, 1)
		uid, _ := intArg(args, 3)
		stake := floatArg(args, 6)
		st.bets = append(st.bets, fakeBet{
			ID: id, MarketID: marketID, UserID: uid,
			Stake: stake, Status: "pending", Sport: "cricket",
			CreatedAt: time.Now(),
		})
		return adminFakeResult{rowsAffected: 1}, nil

	// Ledger insert used by wallet.Deposit + hierarchy.TransferCredit.
	case strings.HasPrefix(q, "insert into ledger"):
		if strings.Contains(q, "'deposit'") || strings.Contains(q, "values ($1, $2, 'deposit'") {
			atomic.AddInt64(&st.depositCalls, 1)
		}
		return adminFakeResult{rowsAffected: 1}, nil
	}

	// Default: pretend it succeeded so unknown admin-side UPDATEs don't fail.
	return adminFakeResult{rowsAffected: 1}, nil
}

func (st *adminFakeState) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	q := normalizeSQL(query)

	switch {
	// --- auth.Service.Register: INSERT ... RETURNING ----------------------
	case strings.HasPrefix(q, "insert into users ("):
		atomic.AddInt64(&st.registerInsertCalls, 1)
		username := strArg(args, 0)
		email := strArg(args, 1)
		role := models.Role(strArg(args, 3))
		var parent *int64
		if id, ok := intArg(args, 4); ok {
			idCopy := id
			parent = &idCopy
		}
		creditLimit := floatArg(args, 5)
		commission := floatArg(args, 6)

		u := &fakeUser{
			Username:       username,
			Email:          email,
			Role:           role,
			ParentID:       parent,
			CreditLimit:    creditLimit,
			CommissionRate: commission,
			Status:         "active",
		}
		u.ID = st.nextUserID
		st.nextUserID++
		u.CreatedAt = time.Now()
		u.UpdatedAt = u.CreatedAt
		st.users[u.ID] = u
		st.usersByName[u.Username] = u.ID

		return &fakeRows{
			cols: []string{"id", "username", "email", "role", "parent_id", "balance",
				"exposure", "credit_limit", "commission_rate", "status", "created_at", "updated_at"},
			rows: [][]driver.Value{{
				u.ID, u.Username, u.Email, string(u.Role), nullableInt64(u.ParentID),
				u.Balance, u.Exposure, u.CreditLimit, u.CommissionRate,
				u.Status, u.CreatedAt, u.UpdatedAt,
			}},
		}, nil

	// --- auth.Service.Register: SELECT path FROM users WHERE id = $1 ------
	case strings.HasPrefix(q, "select path from users where id = $1"):
		id, _ := intArg(args, 0)
		if u, ok := st.users[id]; ok {
			return singleColRow("path", u.Path), nil
		}
		return emptyRows("path"), nil

	// --- wallet.Deposit: SELECT balance FROM users WHERE id = $1 FOR UPDATE
	case strings.HasPrefix(q, "select balance from users where id = $1 for update"):
		id, _ := intArg(args, 0)
		if u, ok := st.users[id]; ok {
			return singleColRow("balance", u.Balance), nil
		}
		return emptyRows("balance"), nil

	// --- Seed.lookupUserID: SELECT id FROM users WHERE username = $1 ------
	case strings.HasPrefix(q, "select id from users where username = $1"):
		name := strArg(args, 0)
		if id, ok := st.usersByName[name]; ok {
			return singleColRow("id", id), nil
		}
		return emptyRows("id"), nil

	// --- Panel dashboard: SELECT username, balance, exposure FROM users WHERE id = $1
	case strings.HasPrefix(q, "select username, balance, exposure from users where id = $1"):
		id, _ := intArg(args, 0)
		if u, ok := st.users[id]; ok {
			return &fakeRows{
				cols: []string{"username", "balance", "exposure"},
				rows: [][]driver.Value{{u.Username, u.Balance, u.Exposure}},
			}, nil
		}
		return emptyRows("username", "balance", "exposure"), nil

	// --- Panel dashboard: SELECT COUNT(*) FROM users WHERE parent_id = $1
	case strings.HasPrefix(q, "select count(*) from users where parent_id = $1"):
		pid, _ := intArg(args, 0)
		n := 0
		for _, u := range st.users {
			if u.ParentID != nil && *u.ParentID == pid {
				n++
			}
		}
		return singleColRow("count", int64(n)), nil

	// --- Panel dashboard: downline count / aggregates -----------------------
	case strings.HasPrefix(q, "select count(*) from users where path <@ (select path from users where id = $1)"):
		id, _ := intArg(args, 0)
		n := int64(0)
		if root, ok := st.users[id]; ok {
			for _, u := range st.users {
				if u.ID == id {
					continue
				}
				if ltreeContains(root.Path, u.Path) {
					n++
				}
			}
		}
		return singleColRow("count", n), nil

	case strings.HasPrefix(q, "select coalesce(sum(balance), 0), coalesce(sum(exposure), 0) from users where path <@"):
		id, _ := intArg(args, 0)
		var bal, exp float64
		if root, ok := st.users[id]; ok {
			for _, u := range st.users {
				if u.ID == id {
					continue
				}
				if ltreeContains(root.Path, u.Path) {
					bal += u.Balance
					exp += u.Exposure
				}
			}
		}
		return &fakeRows{
			cols: []string{"balance", "exposure"},
			rows: [][]driver.Value{{bal, exp}},
		}, nil

	// --- Panel dashboard superadmin platform stats --------------------------
	case q == "select count(*) from users":
		return singleColRow("count", int64(len(st.users))), nil

	case q == "select count(*) from markets":
		return singleColRow("count", int64(0)), nil

	case strings.HasPrefix(q, "select count(*), coalesce(sum(stake), 0) from bets"):
		var stake float64
		for _, b := range st.bets {
			stake += b.Stake
		}
		return &fakeRows{
			cols: []string{"count", "stake"},
			rows: [][]driver.Value{{int64(len(st.bets)), stake}},
		}, nil

	// --- PanelUsers (downline list) -----------------------------------------
	case strings.Contains(q, "from users where path <@") && strings.Contains(q, "order by created_at desc"):
		id, _ := intArg(args, 0)
		root, ok := st.users[id]
		if !ok {
			return emptyUsersRows(), nil
		}
		var filterRole string
		if strings.Contains(q, "and role =") {
			filterRole = strArg(args, 1)
		}
		rows := [][]driver.Value{}
		for _, u := range st.users {
			if u.ID == id {
				continue
			}
			if !ltreeContains(root.Path, u.Path) {
				continue
			}
			if filterRole != "" && string(u.Role) != filterRole {
				continue
			}
			rows = append(rows, userRowValues(u))
		}
		return &fakeRows{cols: userRowCols(), rows: rows}, nil

	// --- PanelUsers (superadmin list) ---------------------------------------
	case strings.HasPrefix(q, "select id, username, email, role, path, parent_id, balance, exposure, credit_limit, commission_rate, status, created_at, updated_at from users"):
		rows := [][]driver.Value{}
		for _, u := range st.users {
			rows = append(rows, userRowValues(u))
		}
		return &fakeRows{cols: userRowCols(), rows: rows}, nil

	// --- PanelAudit (downline join) -----------------------------------------
	case strings.Contains(q, "from audit_log a join users u on u.id = a.actor_id") &&
		strings.Contains(q, "u.path <@"):
		id, _ := intArg(args, 0)
		root, ok := st.users[id]
		if !ok {
			return emptyAuditRows(), nil
		}
		rows := [][]driver.Value{}
		for _, a := range st.audits {
			actor, ok := st.users[a.ActorID]
			if !ok {
				continue
			}
			if !ltreeContains(root.Path, actor.Path) {
				continue
			}
			rows = append(rows, auditRowValues(a))
		}
		return &fakeRows{cols: auditRowCols(), rows: rows}, nil

	// --- PanelAudit (superadmin, unfiltered) --------------------------------
	case strings.HasPrefix(q, "select id, actor_id, action, entity_type, entity_id, ip_address::text, created_at from audit_log"):
		rows := [][]driver.Value{}
		for _, a := range st.audits {
			rows = append(rows, auditRowValues(a))
		}
		return &fakeRows{cols: auditRowCols(), rows: rows}, nil

	// --- PanelPnL -----------------------------------------------------------
	case strings.Contains(q, "to_char(date_trunc('day', b.created_at)") &&
		strings.Contains(q, "from bets b"):
		var id int64
		hasJoin := strings.Contains(q, "join users u on u.id = b.user_id")
		if hasJoin {
			id, _ = intArg(args, 0)
		}
		// Group bets by day
		type daily struct {
			count int
			stake float64
			pnl   float64
		}
		agg := map[string]*daily{}
		for _, b := range st.bets {
			if hasJoin {
				u, ok := st.users[b.UserID]
				if !ok {
					continue
				}
				root := st.users[id]
				if root == nil {
					continue
				}
				if !ltreeContains(root.Path, u.Path) {
					continue
				}
			}
			day := b.CreatedAt.Format("2006-01-02")
			if _, ok := agg[day]; !ok {
				agg[day] = &daily{}
			}
			agg[day].count++
			agg[day].stake += b.Stake
			if b.Status == "settled" {
				agg[day].pnl += b.Profit
			}
		}
		rows := [][]driver.Value{}
		for day, d := range agg {
			rows = append(rows, []driver.Value{day, int64(d.count), d.stake, d.pnl})
		}
		return &fakeRows{
			cols: []string{"day", "count", "stake", "pnl"},
			rows: rows,
		}, nil

	// --- PanelVolume --------------------------------------------------------
	case strings.Contains(q, "coalesce(m.sport, 'unknown')") && strings.Contains(q, "from bets b"):
		var id int64
		hasJoin := strings.Contains(q, "join users u on u.id = b.user_id")
		if hasJoin {
			id, _ = intArg(args, 0)
		}
		agg := map[string]struct {
			count int
			vol   float64
		}{}
		for _, b := range st.bets {
			if hasJoin {
				u, ok := st.users[b.UserID]
				if !ok {
					continue
				}
				root := st.users[id]
				if root == nil {
					continue
				}
				if !ltreeContains(root.Path, u.Path) {
					continue
				}
			}
			sport := b.Sport
			if sport == "" {
				sport = "unknown"
			}
			cur := agg[sport]
			cur.count++
			cur.vol += b.Stake
			agg[sport] = cur
		}
		rows := [][]driver.Value{}
		for sport, d := range agg {
			rows = append(rows, []driver.Value{sport, int64(d.count), d.vol})
		}
		return &fakeRows{
			cols: []string{"sport", "count", "volume"},
			rows: rows,
		}, nil
	}

	// Default: empty result set with no columns.
	return &fakeRows{done: true}, nil
}

func userRowCols() []string {
	return []string{"id", "username", "email", "role", "path", "parent_id",
		"balance", "exposure", "credit_limit", "commission_rate", "status",
		"created_at", "updated_at"}
}

func userRowValues(u *fakeUser) []driver.Value {
	return []driver.Value{
		u.ID, u.Username, u.Email, string(u.Role), u.Path, nullableInt64(u.ParentID),
		u.Balance, u.Exposure, u.CreditLimit, u.CommissionRate,
		u.Status, u.CreatedAt, u.UpdatedAt,
	}
}

func emptyUsersRows() driver.Rows {
	return &fakeRows{cols: userRowCols(), done: true}
}

func auditRowCols() []string {
	return []string{"id", "actor_id", "action", "entity_type", "entity_id", "ip_address", "created_at"}
}

func auditRowValues(a fakeAudit) []driver.Value {
	return []driver.Value{
		a.ID, a.ActorID, a.Action, a.EntityType, a.EntityID, a.IPAddress, a.CreatedAt,
	}
}

func emptyAuditRows() driver.Rows {
	return &fakeRows{cols: auditRowCols(), done: true}
}

// ltreeContains reports whether target is inside the ltree subtree rooted at
// root. For our fake DB paths look like "1.2.4.7" and subtree membership is a
// path-prefix check.
func ltreeContains(root, target string) bool {
	if root == "" {
		return false
	}
	if root == target {
		return true
	}
	return strings.HasPrefix(target, root+".")
}

// ---------------------------------------------------------------------------
// Rows implementation
// ---------------------------------------------------------------------------

type fakeRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
	done bool
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	if r.idx >= len(r.rows) {
		r.done = true
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

func singleColRow(col string, val driver.Value) driver.Rows {
	return &fakeRows{cols: []string{col}, rows: [][]driver.Value{{val}}}
}

func emptyRows(cols ...string) driver.Rows {
	return &fakeRows{cols: cols, done: true}
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func normalizeSQL(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func strArg(args []driver.NamedValue, idx int) string {
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

func intArg(args []driver.NamedValue, idx int) (int64, bool) {
	if idx >= len(args) {
		return 0, false
	}
	switch v := args[idx].Value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case nil:
		return 0, false
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

func nullableInt64(p *int64) driver.Value {
	if p == nil {
		return nil
	}
	return *p
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// seedHierarchy wires up the standard superadmin → admin → master → agent →
// client tree used by most panel tests. Returns the IDs keyed by username.
func seedHierarchy(st *adminFakeState) map[string]int64 {
	sa := &fakeUser{ID: 1, Username: "sa", Email: "sa@x", Role: models.RoleSuperAdmin,
		Path: "1", Balance: 5000, Exposure: 0}
	admin := &fakeUser{ID: 2, Username: "admin", Email: "a@x", Role: models.RoleAdmin,
		Path: "1.2", ParentID: ptrInt64(1), Balance: 3000, Exposure: 0}
	master := &fakeUser{ID: 3, Username: "master", Email: "m@x", Role: models.RoleMaster,
		Path: "1.2.3", ParentID: ptrInt64(2), Balance: 2000, Exposure: 100}
	agent := &fakeUser{ID: 4, Username: "agent", Email: "ag@x", Role: models.RoleAgent,
		Path: "1.2.3.4", ParentID: ptrInt64(3), Balance: 1000, Exposure: 200}
	p1 := &fakeUser{ID: 5, Username: "p1", Email: "p1@x", Role: models.RoleClient,
		Path: "1.2.3.4.5", ParentID: ptrInt64(4), Balance: 500, Exposure: 50}
	p2 := &fakeUser{ID: 6, Username: "p2", Email: "p2@x", Role: models.RoleClient,
		Path: "1.2.3.4.6", ParentID: ptrInt64(4), Balance: 400, Exposure: 25}

	// Outsider lineage that must NOT leak into the agent's downline results.
	outAdmin := &fakeUser{ID: 10, Username: "outadmin", Email: "oa@x", Role: models.RoleAdmin,
		Path: "1.10", ParentID: ptrInt64(1), Balance: 999, Exposure: 0}
	outPlayer := &fakeUser{ID: 11, Username: "outp", Email: "op@x", Role: models.RoleClient,
		Path: "1.10.11", ParentID: ptrInt64(10), Balance: 777, Exposure: 0}

	for _, u := range []*fakeUser{sa, admin, master, agent, p1, p2, outAdmin, outPlayer} {
		st.addUser(u)
	}
	return map[string]int64{
		"sa": 1, "admin": 2, "master": 3, "agent": 4,
		"p1": 5, "p2": 6, "outadmin": 10, "outp": 11,
	}
}

func ptrInt64(v int64) *int64 { return &v }

// newTestHandler returns a Handler with just enough wiring for the panel
// routes. Services that panel handlers don't touch are left nil.
func newTestHandler(db *sql.DB) *Handler {
	return &Handler{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// withUser returns a request whose context carries the legacy per-field
// identity keys so middleware.UserIDFromContext / RoleFromContext resolve.
func withUser(req *http.Request, userID int64, role models.Role) *http.Request {
	ctx := req.Context()
	ctx = context.WithValue(ctx, middleware.UserIDKey, userID)
	ctx = context.WithValue(ctx, middleware.RoleKey, role)
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// Panel handler tests
// ---------------------------------------------------------------------------

func TestPanelDashboard_AsAgent_ReturnsDownlineStats(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/api/v1/panel/dashboard", nil)
	req = withUser(req, ids["agent"], models.RoleAgent)
	rr := httptest.NewRecorder()

	h.PanelDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	// Agent's own balance & role
	if got := body["role"]; got != "agent" {
		t.Errorf("role = %v, want agent", got)
	}
	if got := body["username"]; got != "agent" {
		t.Errorf("username = %v, want agent", got)
	}
	if got := body["own_balance"]; got != float64(1000) {
		t.Errorf("own_balance = %v, want 1000", got)
	}
	if got := body["own_exposure"]; got != float64(200) {
		t.Errorf("own_exposure = %v, want 200", got)
	}
	if got := body["available_balance"]; got != float64(800) {
		t.Errorf("available_balance = %v, want 800", got)
	}

	// Downline: agent has 2 direct children (p1, p2) and 2 total.
	if got := body["direct_children"]; got != float64(2) {
		t.Errorf("direct_children = %v, want 2", got)
	}
	if got := body["downline_total"]; got != float64(2) {
		t.Errorf("downline_total = %v, want 2", got)
	}

	// Downline balance = p1(500) + p2(400) = 900, exposure = 50 + 25 = 75.
	if got := body["downline_balance"]; got != float64(900) {
		t.Errorf("downline_balance = %v, want 900", got)
	}
	if got := body["downline_exposure"]; got != float64(75) {
		t.Errorf("downline_exposure = %v, want 75", got)
	}

	// Non-superadmin must NOT see platform-wide fields.
	if _, ok := body["platform_total_users"]; ok {
		t.Error("non-superadmin leaked platform_total_users")
	}
}

func TestPanelDashboard_AsSuperAdmin_IncludesPlatformStats(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/api/v1/panel/dashboard", nil)
	req = withUser(req, ids["sa"], models.RoleSuperAdmin)
	rr := httptest.NewRecorder()
	h.PanelDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if _, ok := body["platform_total_users"]; !ok {
		t.Error("superadmin missing platform_total_users field")
	}
	if _, ok := body["platform_total_markets"]; !ok {
		t.Error("superadmin missing platform_total_markets")
	}
	if _, ok := body["platform_total_bets"]; !ok {
		t.Error("superadmin missing platform_total_bets")
	}
}

func TestPanelDashboard_AsClient_Returns403(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/api/v1/panel/dashboard", nil)
	req = withUser(req, ids["p1"], models.RoleClient)
	rr := httptest.NewRecorder()
	h.PanelDashboard(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestPanelUsers_ScopedToDownline(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)

	// Agent caller should only see its own subtree (p1, p2), NOT the
	// outsider admin tree hanging off superadmin.
	req := httptest.NewRequest("GET", "/api/v1/panel/users", nil)
	req = withUser(req, ids["agent"], models.RoleAgent)
	rr := httptest.NewRecorder()
	h.PanelUsers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var users []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &users); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("user count = %d, want 2 (p1,p2); body=%s", len(users), rr.Body.String())
	}
	seen := map[string]bool{}
	for _, u := range users {
		seen[u["username"].(string)] = true
	}
	if !seen["p1"] || !seen["p2"] {
		t.Errorf("missing p1/p2: got %v", seen)
	}
	if seen["outadmin"] || seen["outp"] {
		t.Errorf("leaked outsider users: %v", seen)
	}
	if seen["agent"] {
		t.Errorf("self should not be in downline list: %v", seen)
	}
}

func TestPanelUsers_AsClient_Returns403(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/users", nil),
		ids["p1"], models.RoleClient)
	rr := httptest.NewRecorder()
	h.PanelUsers(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestPanelAudit_ScopedToDownline(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	now := time.Now()
	st.audits = []fakeAudit{
		// inside agent's downline
		{ID: 1, ActorID: ids["p1"], Action: "login", EntityType: "user", EntityID: "5",
			IPAddress: "10.0.0.1", CreatedAt: now},
		{ID: 2, ActorID: ids["p2"], Action: "bet_place", EntityType: "bet", EntityID: "bet-1",
			IPAddress: "10.0.0.2", CreatedAt: now},
		// outside agent's downline
		{ID: 3, ActorID: ids["outp"], Action: "login", EntityType: "user", EntityID: "11",
			IPAddress: "10.0.0.3", CreatedAt: now},
	}

	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/audit", nil),
		ids["agent"], models.RoleAgent)
	rr := httptest.NewRecorder()
	h.PanelAudit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var entries []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("audit count = %d, want 2; body=%s", len(entries), rr.Body.String())
	}
	for _, e := range entries {
		actorID := int64(e["actor_id"].(float64))
		if actorID == ids["outp"] {
			t.Errorf("leaked outsider audit: actor_id=%d", actorID)
		}
	}
}

func TestPanelAudit_AsClient_Returns403(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/audit", nil),
		ids["p1"], models.RoleClient)
	rr := httptest.NewRecorder()
	h.PanelAudit(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestPanelPnL_ScopedToDownline(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	now := time.Now()
	st.bets = []fakeBet{
		{ID: "b1", MarketID: "m1", UserID: ids["p1"], Stake: 100, Profit: 50,
			Status: "settled", Sport: "cricket", CreatedAt: now},
		{ID: "b2", MarketID: "m1", UserID: ids["p2"], Stake: 200, Profit: -30,
			Status: "settled", Sport: "football", CreatedAt: now},
		// Outsider bet — must NOT appear in agent's PnL
		{ID: "b3", MarketID: "m1", UserID: ids["outp"], Stake: 999, Profit: 999,
			Status: "settled", Sport: "cricket", CreatedAt: now},
	}

	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/reports/pnl", nil),
		ids["agent"], models.RoleAgent)
	rr := httptest.NewRecorder()
	h.PanelPnL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var report []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(report) != 1 {
		t.Fatalf("days = %d, want 1; body=%s", len(report), rr.Body.String())
	}
	row := report[0]
	if got := row["bets"]; got != float64(2) {
		t.Errorf("bets = %v, want 2 (outsider excluded)", got)
	}
	if got := row["stake"]; got != float64(300) {
		t.Errorf("stake = %v, want 300", got)
	}
	if got := row["pnl"]; got != float64(20) {
		t.Errorf("pnl = %v, want 20 (50 + -30)", got)
	}
}

func TestPanelPnL_AsClient_Returns403(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)
	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/reports/pnl", nil),
		ids["p1"], models.RoleClient)
	rr := httptest.NewRecorder()
	h.PanelPnL(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestPanelVolume_ScopedToDownline(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)

	now := time.Now()
	st.bets = []fakeBet{
		{ID: "b1", MarketID: "m1", UserID: ids["p1"], Stake: 100,
			Status: "pending", Sport: "cricket", CreatedAt: now},
		{ID: "b2", MarketID: "m2", UserID: ids["p2"], Stake: 200,
			Status: "pending", Sport: "football", CreatedAt: now},
		{ID: "b3", MarketID: "m1", UserID: ids["p1"], Stake: 50,
			Status: "pending", Sport: "cricket", CreatedAt: now},
		// Outsider bet — must NOT appear in agent's volume
		{ID: "b4", MarketID: "m9", UserID: ids["outp"], Stake: 999,
			Status: "pending", Sport: "tennis", CreatedAt: now},
	}

	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/reports/volume", nil),
		ids["agent"], models.RoleAgent)
	rr := httptest.NewRecorder()
	h.PanelVolume(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var report []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Expect 2 sports: cricket (150), football (200). Tennis must NOT appear.
	vols := map[string]float64{}
	bets := map[string]float64{}
	for _, r := range report {
		sport := r["sport"].(string)
		vols[sport] = r["volume"].(float64)
		bets[sport] = r["bets"].(float64)
	}
	if vols["cricket"] != 150 {
		t.Errorf("cricket volume = %v, want 150", vols["cricket"])
	}
	if vols["football"] != 200 {
		t.Errorf("football volume = %v, want 200", vols["football"])
	}
	if _, leaked := vols["tennis"]; leaked {
		t.Errorf("leaked tennis volume from outsider bet: %v", vols["tennis"])
	}
	if bets["cricket"] != 2 {
		t.Errorf("cricket bets = %v, want 2", bets["cricket"])
	}
}

func TestPanelVolume_AsClient_Returns403(t *testing.T) {
	db, st := registerAdminFakeDB(t)
	ids := seedHierarchy(st)
	h := newTestHandler(db)
	req := withUser(httptest.NewRequest("GET", "/api/v1/panel/reports/volume", nil),
		ids["p1"], models.RoleClient)
	rr := httptest.NewRecorder()
	h.PanelVolume(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}
