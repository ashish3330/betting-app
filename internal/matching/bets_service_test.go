package matching

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fake database/sql driver for bets_service_test
//
// The bets_service.go read paths all funnel through queryEnrichedBets, plus
// a dedicated two-step query inside GetUserPositionsForMarket. We don't pull
// in go-sqlmock because the rest of the service tree avoids extra test
// dependencies; see internal/wallet/service_test.go for the same pattern.
//
// The driver recognises only the shapes it needs:
//
//   - SELECT ... FROM betting.bets b LEFT JOIN betting.markets m ...
//     → returns pre-seeded enriched bet rows.
//   - SELECT name, market_type FROM betting.markets WHERE id = $1
//     → returns the seeded market metadata.
//   - SELECT b.selection_id, ..., exposure FROM betting.bets ... GROUP BY
//     → returns seeded position rows.
//
// Anything else falls through to an empty result.
// ---------------------------------------------------------------------------

func init() {
	sql.Register("matchingfake", &bsFakeDriver{})
}

// bsFakeState is the per-test fixture, keyed by DSN in bsStateRegistry.
type bsFakeState struct {
	mu sync.Mutex

	// Bets returned by queryEnrichedBets for ANY user — tests that care
	// about per-user scoping seed bets for the specific userID they pass
	// to the service and rely on the dispatcher checking the user_id arg.
	betsByUser map[int64][]EnrichedBet

	// Market metadata returned by the SELECT ... FROM betting.markets
	// query inside GetUserPositionsForMarket.
	markets map[string]marketMeta

	// Positions returned by GetUserPositionsForMarket's second query,
	// keyed by (userID, marketID).
	positions map[positionKey][]positionRow
}

type marketMeta struct {
	Name       string
	MarketType string
}

type positionKey struct {
	userID   int64
	marketID string
}

type positionRow struct {
	SelectionID   int64
	SelectionName string
	BackStake     float64
	LayStake      float64
	Exposure      float64
}

func newBSFakeState() *bsFakeState {
	return &bsFakeState{
		betsByUser: map[int64][]EnrichedBet{},
		markets:    map[string]marketMeta{},
		positions:  map[positionKey][]positionRow{},
	}
}

var bsStateRegistry sync.Map

func registerBSFakeDB(t *testing.T) (*sql.DB, *bsFakeState) {
	t.Helper()
	st := newBSFakeState()
	dsn := t.Name() + "/" + time.Now().Format(time.RFC3339Nano)
	bsStateRegistry.Store(dsn, st)
	t.Cleanup(func() { bsStateRegistry.Delete(dsn) })

	db, err := sql.Open("matchingfake", dsn)
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, st
}

type bsFakeDriver struct{}

func (d *bsFakeDriver) Open(name string) (driver.Conn, error) {
	st, ok := bsStateRegistry.Load(name)
	if !ok {
		return nil, errors.New("matchingfake: unknown DSN " + name)
	}
	return &bsFakeConn{state: st.(*bsFakeState)}, nil
}

type bsFakeConn struct {
	state *bsFakeState
}

func (c *bsFakeConn) Prepare(query string) (driver.Stmt, error) {
	return &bsFakeStmt{conn: c, query: query}, nil
}
func (c *bsFakeConn) Close() error { return nil }
func (c *bsFakeConn) Begin() (driver.Tx, error) {
	return &bsFakeTx{}, nil
}
func (c *bsFakeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return &bsFakeTx{}, nil
}
func (c *bsFakeConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return bsFakeResult{}, nil
}
func (c *bsFakeConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(q, args)
}

type bsFakeTx struct{}

func (bsFakeTx) Commit() error   { return nil }
func (bsFakeTx) Rollback() error { return nil }

type bsFakeStmt struct {
	conn  *bsFakeConn
	query string
}

func (s *bsFakeStmt) Close() error  { return nil }
func (s *bsFakeStmt) NumInput() int { return -1 }
func (s *bsFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return bsFakeResult{}, nil
}
func (s *bsFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.state.query(s.query, named)
}
func (s *bsFakeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return bsFakeResult{}, nil
}
func (s *bsFakeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.state.query(s.query, args)
}

type bsFakeResult struct{}

func (bsFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (bsFakeResult) RowsAffected() (int64, error) { return 0, nil }

// ---------------------------------------------------------------------------
// Query dispatch
// ---------------------------------------------------------------------------

func (st *bsFakeState) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	q := strings.ToLower(strings.Join(strings.Fields(query), " "))

	switch {
	// Positions group-by query: pick it up BEFORE the generic betting.bets
	// branch because it also mentions FROM betting.bets.
	case strings.Contains(q, "from betting.bets b") && strings.Contains(q, "group by"):
		uid, _ := bsInt64Arg(args, 0)
		mid, _ := bsStringArg(args, 1)
		rows := st.positions[positionKey{userID: uid, marketID: mid}]
		return &bsPositionRows{rows: rows}, nil

	// Enriched bets listing query used by queryEnrichedBets.
	case strings.Contains(q, "from betting.bets b") && strings.Contains(q, "order by b.created_at desc"):
		uid, _ := bsInt64Arg(args, 0)
		return &bsBetRows{bets: st.betsByUser[uid]}, nil

	// Market metadata SELECT from GetUserPositionsForMarket.
	case strings.Contains(q, "select name, market_type from betting.markets"):
		mid, _ := bsStringArg(args, 0)
		m, ok := st.markets[mid]
		if !ok {
			return &bsSingleRow{done: true, cols: []string{"name", "market_type"}}, nil
		}
		return &bsSingleRow{
			cols: []string{"name", "market_type"},
			vals: []driver.Value{m.Name, m.MarketType},
		}, nil
	}

	return &bsSingleRow{done: true}, nil
}

// bsSingleRow returns at most one row.
type bsSingleRow struct {
	cols []string
	vals []driver.Value
	done bool
}

func (r *bsSingleRow) Columns() []string { return r.cols }
func (r *bsSingleRow) Close() error      { return nil }
func (r *bsSingleRow) Next(dest []driver.Value) error {
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

// bsBetRows serialises an []EnrichedBet into the 17 scan columns emitted by
// queryEnrichedBets (see internal/matching/bets_service.go).
type bsBetRows struct {
	bets []EnrichedBet
	idx  int
}

func (r *bsBetRows) Columns() []string {
	return []string{
		"id", "market_id", "selection_id", "user_id",
		"side", "price", "stake", "matched_stake", "unmatched_stake", "profit",
		"status", "client_ref", "created_at",
		"market_name", "selection_name", "market_type", "display_side",
	}
}

func (r *bsBetRows) Close() error { return nil }

func (r *bsBetRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.bets) {
		return io.EOF
	}
	b := r.bets[r.idx]
	r.idx++
	dest[0] = b.ID
	dest[1] = b.MarketID
	dest[2] = b.SelectionID
	dest[3] = b.UserID
	dest[4] = b.Side
	dest[5] = b.Price
	dest[6] = b.Stake
	dest[7] = b.MatchedStake
	dest[8] = b.UnmatchedStake
	dest[9] = b.Profit
	dest[10] = b.Status
	dest[11] = b.ClientRef
	dest[12] = b.CreatedAt
	dest[13] = b.MarketName
	dest[14] = b.SelectionName
	dest[15] = b.MarketType
	dest[16] = b.DisplaySide
	return nil
}

// bsPositionRows serialises positionRow entries for the GROUP BY query in
// GetUserPositionsForMarket. Columns match the 5-column projection:
// selection_id, selection_name, back_stake, lay_stake, exposure.
type bsPositionRows struct {
	rows []positionRow
	idx  int
}

func (r *bsPositionRows) Columns() []string {
	return []string{"selection_id", "selection_name", "back_stake", "lay_stake", "exposure"}
}
func (r *bsPositionRows) Close() error { return nil }
func (r *bsPositionRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	p := r.rows[r.idx]
	r.idx++
	dest[0] = p.SelectionID
	dest[1] = p.SelectionName
	dest[2] = p.BackStake
	dest[3] = p.LayStake
	dest[4] = p.Exposure
	return nil
}

func bsStringArg(args []driver.NamedValue, idx int) (string, bool) {
	if idx >= len(args) {
		return "", false
	}
	switch v := args[idx].Value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	}
	return "", false
}

func bsInt64Arg(args []driver.NamedValue, idx int) (int64, bool) {
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestMatchingHandler(t *testing.T) (*Handler, *bsFakeState) {
	t.Helper()
	db, st := registerBSFakeDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Handler{db: db, logger: logger}, st
}

// sampleBets returns a small fixture covering the combinations required by
// the listing/history/position tests.
func sampleBets() []EnrichedBet {
	base := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	return []EnrichedBet{
		{
			ID: "b1", MarketID: "mkt-1", SelectionID: 1, UserID: 42,
			Side: "back", Price: 2.0, Stake: 100, MatchedStake: 100, UnmatchedStake: 0,
			Profit: 100, Status: "settled", CreatedAt: base.Add(5 * time.Minute),
			MarketName: "Match Odds", SelectionName: "Home", MarketType: "match_odds",
		},
		{
			ID: "b2", MarketID: "mkt-1", SelectionID: 2, UserID: 42,
			Side: "lay", Price: 3.0, Stake: 50, MatchedStake: 50, UnmatchedStake: 0,
			Profit: -100, Status: "settled", CreatedAt: base.Add(4 * time.Minute),
			MarketName: "Match Odds", SelectionName: "Away", MarketType: "match_odds",
		},
		{
			ID: "b3", MarketID: "mkt-2", SelectionID: 1, UserID: 42,
			Side: "back", Price: 2.5, Stake: 200, MatchedStake: 200, UnmatchedStake: 0,
			Profit: 0, Status: "matched", CreatedAt: base.Add(3 * time.Minute),
			MarketName: "Next Goal", SelectionName: "Team A", MarketType: "match_odds",
		},
		{
			ID: "b4", MarketID: "mkt-2", SelectionID: 2, UserID: 42,
			Side: "back", Price: 4.0, Stake: 25, MatchedStake: 25, UnmatchedStake: 0,
			Profit: 0, Status: "cancelled", CreatedAt: base.Add(2 * time.Minute),
			MarketName: "Next Goal", SelectionName: "Team B", MarketType: "match_odds",
		},
		{
			ID: "b5", MarketID: "mkt-1", SelectionID: 1, UserID: 42,
			Side: "back", Price: 3.0, Stake: 10, MatchedStake: 10, UnmatchedStake: 0,
			Profit: 0, Status: "void", CreatedAt: base.Add(1 * time.Minute),
			MarketName: "Match Odds", SelectionName: "Home", MarketType: "match_odds",
		},
		// Placed bet that should be filtered out of user-facing listings.
		{
			ID: "b6", MarketID: "mkt-1", SelectionID: 1, UserID: 42,
			Side: "back", Price: 2.0, Stake: 5, MatchedStake: 0, UnmatchedStake: 5,
			Profit: 0, Status: "pending", CreatedAt: base,
			MarketName: "Match Odds", SelectionName: "Home", MarketType: "match_odds",
		},
	}
}

// ---------------------------------------------------------------------------
// ListUserBets tests
// ---------------------------------------------------------------------------

func TestListUserBets_StatusFilter(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	st.betsByUser[42] = sampleBets()

	// ?status=open must be aliased to "matched".
	openResult, err := h.ListUserBets(context.Background(), 42, "open", "", 1, 50)
	if err != nil {
		t.Fatalf("ListUserBets open: %v", err)
	}
	if len(openResult.Bets) != 1 {
		t.Fatalf("open filter: got %d bets, want 1", len(openResult.Bets))
	}
	if openResult.Bets[0].ID != "b3" || openResult.Bets[0].Status != "matched" {
		t.Fatalf("open filter: got %+v, want the single matched bet b3", openResult.Bets[0])
	}

	// ?status=cancelled must return only cancelled bets.
	cancelledResult, err := h.ListUserBets(context.Background(), 42, "cancelled", "", 1, 50)
	if err != nil {
		t.Fatalf("ListUserBets cancelled: %v", err)
	}
	if len(cancelledResult.Bets) != 1 {
		t.Fatalf("cancelled filter: got %d bets, want 1", len(cancelledResult.Bets))
	}
	if cancelledResult.Bets[0].ID != "b4" || cancelledResult.Bets[0].Status != "cancelled" {
		t.Fatalf("cancelled filter: got %+v, want b4 (cancelled)", cancelledResult.Bets[0])
	}

	// Pending/unmatched must never leak through the valid-status gate even
	// when no filter is provided.
	allResult, err := h.ListUserBets(context.Background(), 42, "", "", 1, 50)
	if err != nil {
		t.Fatalf("ListUserBets all: %v", err)
	}
	for _, b := range allResult.Bets {
		if b.Status == "pending" || b.Status == "unmatched" || b.Status == "partial" {
			t.Fatalf("listing leaked in-flight bet %q (status=%s)", b.ID, b.Status)
		}
	}
}

func TestListUserBets_MarketFilter(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	st.betsByUser[42] = sampleBets()

	result, err := h.ListUserBets(context.Background(), 42, "", "mkt-2", 1, 50)
	if err != nil {
		t.Fatalf("ListUserBets market filter: %v", err)
	}
	// mkt-2 has b3 (matched) and b4 (cancelled). mkt-2/b6 doesn't exist.
	if len(result.Bets) != 2 {
		t.Fatalf("market filter: got %d bets, want 2", len(result.Bets))
	}
	for _, b := range result.Bets {
		if b.MarketID != "mkt-2" {
			t.Fatalf("market filter leaked bet for market %q", b.MarketID)
		}
	}
}

func TestListUserBets_Pagination(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	st.betsByUser[42] = sampleBets()
	// Valid-status bets are: b1, b2, b3, b4, b5 = 5 total.

	page1, err := h.ListUserBets(context.Background(), 42, "", "", 1, 2)
	if err != nil {
		t.Fatalf("ListUserBets page 1: %v", err)
	}
	if got := len(page1.Bets); got != 2 {
		t.Fatalf("page 1: got %d bets, want 2", got)
	}
	if page1.Total != 5 {
		t.Fatalf("page 1 total: got %d, want 5", page1.Total)
	}
	if page1.Limit != 2 || page1.Page != 1 {
		t.Fatalf("page 1 envelope: page=%d limit=%d", page1.Page, page1.Limit)
	}

	page2, err := h.ListUserBets(context.Background(), 42, "", "", 2, 2)
	if err != nil {
		t.Fatalf("ListUserBets page 2: %v", err)
	}
	if got := len(page2.Bets); got != 2 {
		t.Fatalf("page 2: got %d bets, want 2", got)
	}
	// Page 2 must not repeat page 1's rows.
	for _, b := range page2.Bets {
		for _, prev := range page1.Bets {
			if b.ID == prev.ID {
				t.Fatalf("page 2 repeated bet %q from page 1", b.ID)
			}
		}
	}

	page3, err := h.ListUserBets(context.Background(), 42, "", "", 3, 2)
	if err != nil {
		t.Fatalf("ListUserBets page 3: %v", err)
	}
	if got := len(page3.Bets); got != 1 {
		t.Fatalf("page 3: got %d bets, want 1 (tail)", got)
	}

	// Past-end page must return an empty slice, not an error.
	page4, err := h.ListUserBets(context.Background(), 42, "", "", 4, 2)
	if err != nil {
		t.Fatalf("ListUserBets page 4: %v", err)
	}
	if len(page4.Bets) != 0 {
		t.Fatalf("page 4: got %d bets, want 0", len(page4.Bets))
	}
}

// ---------------------------------------------------------------------------
// BetsHistory tests
// ---------------------------------------------------------------------------

func TestBetsHistory_Summary(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	st.betsByUser[42] = sampleBets()

	result, err := h.BetsHistory(context.Background(), 42)
	if err != nil {
		t.Fatalf("BetsHistory: %v", err)
	}

	// Valid-status bets: b1 settled +100, b2 settled -100, b3 matched,
	// b4 cancelled, b5 void. pending (b6) is excluded.
	if result.Summary.TotalBets != 5 {
		t.Fatalf("TotalBets = %d, want 5", result.Summary.TotalBets)
	}
	// total_stake = 100+50+200+25+10 = 385
	if result.Summary.TotalStake != 385 {
		t.Fatalf("TotalStake = %v, want 385", result.Summary.TotalStake)
	}
	// total_pnl = 100 + (-100) = 0 (only settled contribute)
	if result.Summary.TotalPnL != 0 {
		t.Fatalf("TotalPnL = %v, want 0", result.Summary.TotalPnL)
	}
	if result.Summary.Won != 1 {
		t.Fatalf("Won = %d, want 1", result.Summary.Won)
	}
	if result.Summary.Lost != 1 {
		t.Fatalf("Lost = %d, want 1", result.Summary.Lost)
	}
	// Pending counts "matched" bets (b3). cancelled/void do not count.
	if result.Summary.Pending != 1 {
		t.Fatalf("Pending = %d, want 1", result.Summary.Pending)
	}
}

// ---------------------------------------------------------------------------
// GetUserPositionsForMarket tests
// ---------------------------------------------------------------------------

func TestGetUserPositionsForMarket_LayLiability(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	const (
		userID   = int64(7)
		marketID = "mkt-lay"
	)
	st.markets[marketID] = marketMeta{Name: "Final Score", MarketType: "match_odds"}
	// A pure lay: stake 100 at price 3.0 ⇒ liability = 100 * (3-1) = 200.
	// The fake mirrors what the SQL CASE expression computes server-side.
	st.positions[positionKey{userID: userID, marketID: marketID}] = []positionRow{
		{SelectionID: 1, SelectionName: "Home", BackStake: 0, LayStake: 100, Exposure: -200},
	}

	result, err := h.GetUserPositionsForMarket(context.Background(), userID, marketID)
	if err != nil {
		t.Fatalf("GetUserPositionsForMarket: %v", err)
	}
	if len(result.Positions) != 1 {
		t.Fatalf("positions: got %d, want 1", len(result.Positions))
	}
	p := result.Positions[0]
	// The SQL encodes a lay's open exposure as -stake*(price-1). What matters
	// for the caller is that the magnitude is the full liability, not just
	// the stake.
	if p.Exposure != -200 {
		t.Fatalf("lay exposure = %v, want -200 (liability = stake*(price-1))", p.Exposure)
	}
	if p.LayStake != 100 {
		t.Fatalf("lay stake = %v, want 100", p.LayStake)
	}
	if p.BackStake != 0 {
		t.Fatalf("lay position should have zero back stake, got %v", p.BackStake)
	}
}

func TestGetUserPositionsForMarket_BackPosition(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	const (
		userID   = int64(7)
		marketID = "mkt-back"
	)
	st.markets[marketID] = marketMeta{Name: "Final Score", MarketType: "match_odds"}
	// Pure back: stake 100 at price 2.5 ⇒ profit if wins = 100*(2.5-1) = 150.
	st.positions[positionKey{userID: userID, marketID: marketID}] = []positionRow{
		{SelectionID: 1, SelectionName: "Home", BackStake: 100, LayStake: 0, Exposure: 150},
	}

	result, err := h.GetUserPositionsForMarket(context.Background(), userID, marketID)
	if err != nil {
		t.Fatalf("GetUserPositionsForMarket: %v", err)
	}
	if len(result.Positions) != 1 {
		t.Fatalf("positions: got %d, want 1", len(result.Positions))
	}
	p := result.Positions[0]
	if p.BackStake != 100 {
		t.Fatalf("back stake = %v, want 100", p.BackStake)
	}
	if p.Exposure != 150 {
		t.Fatalf("back exposure = %v, want 150 (stake*(price-1))", p.Exposure)
	}
	if p.NetStake != 100 {
		t.Fatalf("net stake = %v, want 100 (back - lay)", p.NetStake)
	}
}

func TestGetUserPositionsForMarket_NetExposure(t *testing.T) {
	h, st := newTestMatchingHandler(t)
	const (
		userID   = int64(7)
		marketID = "mkt-net"
	)
	st.markets[marketID] = marketMeta{Name: "Final Score", MarketType: "match_odds"}
	// Same selection, both sides:
	//   back: stake 100 @ 2.0 → profit-if-wins = 100*(2-1) = 100
	//   lay:  stake  50 @ 2.0 → liability      = -50*(2-1) = -50
	// Net exposure = 100 - 50 = 50 (user still wins 50 on this selection).
	st.positions[positionKey{userID: userID, marketID: marketID}] = []positionRow{
		{SelectionID: 1, SelectionName: "Home", BackStake: 100, LayStake: 50, Exposure: 50},
	}

	result, err := h.GetUserPositionsForMarket(context.Background(), userID, marketID)
	if err != nil {
		t.Fatalf("GetUserPositionsForMarket: %v", err)
	}
	if len(result.Positions) != 1 {
		t.Fatalf("positions: got %d, want 1", len(result.Positions))
	}
	p := result.Positions[0]
	if p.BackStake != 100 || p.LayStake != 50 {
		t.Fatalf("back/lay stake = %v/%v, want 100/50", p.BackStake, p.LayStake)
	}
	if p.NetStake != 50 {
		t.Fatalf("net stake = %v, want 50 (back 100 - lay 50)", p.NetStake)
	}
	if p.Exposure != 50 {
		t.Fatalf("net exposure = %v, want 50", p.Exposure)
	}
}

// Empty market_id must return an error up front.
func TestGetUserPositionsForMarket_RequiresMarketID(t *testing.T) {
	h, _ := newTestMatchingHandler(t)
	if _, err := h.GetUserPositionsForMarket(context.Background(), 1, ""); err == nil {
		t.Fatal("expected error for empty market_id")
	}
}
