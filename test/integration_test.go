//go:build integration

// Package integration boots the real HTTP stack against a real Postgres and
// reproduces the grader's correctness gate: concurrent first-transfers,
// concurrent same-key retries, conservation, and the edge-case matrix.
//
// Run with:  go test -tags integration ./test/
// Uses DATABASE_URL if set (e.g. the compose db); otherwise it starts an
// embedded Postgres — no local setup needed.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/app"
	"wallet-service/internal/config"
	"wallet-service/internal/db"
	"wallet-service/internal/obs"
)

const seedPaise = 100000

var (
	server   *httptest.Server
	testPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup:", err)
		code = 1
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		epg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(54329).
			Username("wallet").Password("wallet").Database("wallet_test").
			RuntimePath(filepath.Join(os.TempDir(), "wallet-epg")).
			Logger(io.Discard))
		if err := epg.Start(); err != nil {
			return 0, fmt.Errorf("start embedded postgres: %w", err)
		}
		defer epg.Stop()
		url = "postgres://wallet:wallet@127.0.0.1:54329/wallet_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	testPool = pool

	// Fresh schema every run so assertions are exact.
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return 0, err
	}
	if err := db.Migrate(url); err != nil {
		return 0, err
	}

	cfg := config.Config{
		JWTSecret:           "integration-test-secret-123",
		InitialBalancePaise: seedPaise,
		TokenTTL:            time.Hour,
		ClaimWindow:         24 * time.Hour,
	}
	ring := obs.NewRing(100)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server = httptest.NewServer(app.NewHandler(cfg, pool, logger, obs.NewMetrics(), ring))
	defer server.Close()

	return m.Run(), nil
}

// ---- HTTP helpers -------------------------------------------------------

type resp struct {
	status int
	header http.Header
	body   map[string]any
}

func call(t *testing.T, method, path, token string, payload any) resp {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out := resp{status: res.StatusCode, header: res.Header, body: map[string]any{}}
	json.NewDecoder(res.Body).Decode(&out.body)
	return out
}

var userSeq int
var userSeqMu sync.Mutex

func signup(t *testing.T) (username, token string) {
	t.Helper()
	userSeqMu.Lock()
	userSeq++
	username = fmt.Sprintf("user%d_%d", userSeq, time.Now().UnixNano()%1e6)
	userSeqMu.Unlock()
	r := call(t, "POST", "/auth/signup", "", map[string]string{
		"username": username, "password": "password123",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("signup %s: status %d body %v", username, r.status, r.body)
	}
	return username, r.body["token"].(string)
}

func transfer(t *testing.T, token, to string, amount int64, key string) resp {
	t.Helper()
	return call(t, "POST", "/transfers", token, map[string]any{
		"to_user": to, "amount_paise": amount, "idempotency_key": key,
	})
}

func balance(t *testing.T, token string) int64 {
	t.Helper()
	r := call(t, "GET", "/accounts/me", token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("GET /accounts/me: status %d body %v", r.status, r.body)
	}
	return int64(r.body["balance_paise"].(float64))
}

func availableBalance(t *testing.T, token string) int64 {
	t.Helper()
	r := call(t, "GET", "/accounts/me", token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("GET /accounts/me: status %d body %v", r.status, r.body)
	}
	return int64(r.body["available_paise"].(float64))
}

func claim(t *testing.T, token, transferID string) resp {
	t.Helper()
	return call(t, "POST", "/transfers/"+transferID+"/claim", token, nil)
}

// backdateTransfer ages a transfer past the claim window; the window check
// runs on the database clock, so shifting created_at is the only knob the
// tests need — no injected clocks.
func backdateTransfer(t *testing.T, transferID string, age time.Duration) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`UPDATE transfers SET created_at = created_at - make_interval(secs => $2) WHERE id = $1`,
		transferID, age.Seconds())
	if err != nil {
		t.Fatalf("backdate transfer: %v", err)
	}
}

func walletRowCount(t *testing.T, username string) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM wallets w JOIN users u ON u.id = w.user_id WHERE u.username = $1`,
		username).Scan(&n)
	if err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	return n
}

func runConcurrent(n int, fn func(i int) resp) []resp {
	results := make([]resp, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // maximize overlap
			results[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return results
}

func no5xx(t *testing.T, results []resp, label string) {
	t.Helper()
	for _, r := range results {
		if r.status >= 500 {
			t.Errorf("%s: got %d (%v) — must never 5xx under concurrency", label, r.status, r.body)
		}
	}
}

// ---- The grader gate ----------------------------------------------------

// Two brand-new users, concurrent first-transfers in BOTH directions:
// wallets created exactly once, no deadlock 5xx, money conserved.
func TestConcurrentFirstTransfersBidirectional(t *testing.T) {
	alice, aTok := signup(t)
	bob, bTok := signup(t)

	const perSide, amount = 20, 10
	results := runConcurrent(2*perSide, func(i int) resp {
		if i < perSide {
			return transfer(t, aTok, bob, amount, fmt.Sprintf("a2b-%d", i))
		}
		return transfer(t, bTok, alice, amount, fmt.Sprintf("b2a-%d", i-perSide))
	})

	no5xx(t, results, "bidirectional first transfers")
	for _, r := range results {
		if r.status != http.StatusCreated {
			t.Errorf("want 201, got %d (%v)", r.status, r.body)
		}
	}
	if n := walletRowCount(t, alice); n != 1 {
		t.Errorf("alice wallet rows = %d, want exactly 1", n)
	}
	if n := walletRowCount(t, bob); n != 1 {
		t.Errorf("bob wallet rows = %d, want exactly 1", n)
	}
	// Equal traffic both ways: both balances must return to seed.
	if a, b := balance(t, aTok), balance(t, bTok); a != seedPaise || b != seedPaise {
		t.Errorf("balances after symmetric crossfire: alice=%d bob=%d, want %d each", a, b, seedPaise)
	}
}

// Many concurrent retries with the SAME idempotency key: money moves once,
// every caller gets the same outcome.
func TestConcurrentSameKeyRetries(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)

	const n, amount = 50, 777
	key := "shared-key-1"
	results := runConcurrent(n, func(int) resp {
		return transfer(t, aTok, bob, amount, key)
	})

	no5xx(t, results, "same-key retries")
	var created, replayed int
	ids := map[any]bool{}
	for _, r := range results {
		switch r.status {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			replayed++
		default:
			t.Errorf("unexpected status %d (%v)", r.status, r.body)
		}
		ids[r.body["transfer_id"]] = true
		if nb := int64(r.body["new_balance"].(float64)); nb != seedPaise-amount {
			t.Errorf("new_balance = %d, want %d", nb, seedPaise-amount)
		}
	}
	if created != 1 {
		t.Errorf("created = %d, want exactly 1", created)
	}
	if replayed != n-1 {
		t.Errorf("replayed = %d, want %d", replayed, n-1)
	}
	if len(ids) != 1 {
		t.Errorf("distinct transfer_ids = %d, want 1", len(ids))
	}
	if a, b := balance(t, aTok), balance(t, bTok); a != seedPaise-amount || b != seedPaise+amount {
		t.Errorf("balances: sender=%d recipient=%d, want %d/%d", a, b, seedPaise-amount, seedPaise+amount)
	}
}

// Random concurrent burst across several users: total money is conserved.
func TestConservationUnderRandomBurst(t *testing.T) {
	const users, burst = 4, 100
	names := make([]string, users)
	tokens := make([]string, users)
	for i := range users {
		names[i], tokens[i] = signup(t)
		call(t, "POST", "/accounts", tokens[i], nil)
	}

	rng := rand.New(rand.NewSource(42))
	type pair struct {
		from   int
		to     string
		amount int64
	}
	plan := make([]pair, burst)
	for i := range plan {
		from := rng.Intn(users)
		to := (from + 1 + rng.Intn(users-1)) % users
		plan[i] = pair{from: from, to: names[to], amount: int64(1 + rng.Intn(1000))}
	}

	results := runConcurrent(burst, func(i int) resp {
		return transfer(t, tokens[plan[i].from], plan[i].to, plan[i].amount, fmt.Sprintf("burst-%d", i))
	})

	no5xx(t, results, "random burst")
	for _, r := range results {
		if r.status != http.StatusCreated && r.status != http.StatusUnprocessableEntity {
			t.Errorf("unexpected status %d (%v)", r.status, r.body)
		}
	}
	var total int64
	for _, tok := range tokens {
		total += balance(t, tok)
	}
	if total != users*seedPaise {
		t.Errorf("total money = %d, want %d — conservation violated", total, users*seedPaise)
	}
}

// POST /accounts is get-or-create: concurrent calls yield one wallet row.
func TestAccountsGetOrCreateIdempotent(t *testing.T) {
	name, tok := signup(t)
	results := runConcurrent(20, func(int) resp {
		return call(t, "POST", "/accounts", tok, nil)
	})
	no5xx(t, results, "concurrent POST /accounts")
	for _, r := range results {
		if r.status != http.StatusOK && r.status != http.StatusCreated {
			t.Errorf("unexpected status %d (%v)", r.status, r.body)
		}
		if b := int64(r.body["balance_paise"].(float64)); b != seedPaise {
			t.Errorf("balance = %d, want seed %d", b, seedPaise)
		}
	}
	if n := walletRowCount(t, name); n != 1 {
		t.Errorf("wallet rows = %d, want exactly 1", n)
	}
}

// ---- Replay semantics ----------------------------------------------------

func TestReplayAndConflict(t *testing.T) {
	_, aTok := signup(t)
	bob, _ := signup(t)

	first := transfer(t, aTok, bob, 500, "k1")
	if first.status != http.StatusCreated {
		t.Fatalf("first transfer: %d (%v)", first.status, first.body)
	}

	replay := transfer(t, aTok, bob, 500, "k1")
	if replay.status != http.StatusOK {
		t.Errorf("replay status = %d, want 200", replay.status)
	}
	if replay.header.Get("Idempotent-Replay") != "true" {
		t.Error("replay must set Idempotent-Replay: true")
	}
	if replay.body["transfer_id"] != first.body["transfer_id"] {
		t.Error("replay must return the original transfer_id")
	}

	conflict := transfer(t, aTok, bob, 999, "k1") // same key, different body
	if conflict.status != http.StatusConflict {
		t.Errorf("same key different body: status = %d, want 409", conflict.status)
	}
}

// A replay must return the ORIGINAL outcome even after the sender's balance
// has dropped below the original amount — proving the idempotency check
// sits before the funds check.
func TestReplayAfterBalanceDrop(t *testing.T) {
	_, aTok := signup(t)
	bob, _ := signup(t)

	first := transfer(t, aTok, bob, 60000, "big")
	if first.status != http.StatusCreated {
		t.Fatalf("first: %d (%v)", first.status, first.body)
	}
	second := transfer(t, aTok, bob, 39000, "drain")
	if second.status != http.StatusCreated {
		t.Fatalf("drain: %d (%v)", second.status, second.body)
	}
	// Balance is now 1000 < 60000; the retry must still replay, not 422.
	replay := transfer(t, aTok, bob, 60000, "big")
	if replay.status != http.StatusOK {
		t.Errorf("replay after balance drop: status = %d, want 200", replay.status)
	}
	if nb := int64(replay.body["new_balance"].(float64)); nb != seedPaise-60000 {
		t.Errorf("replayed new_balance = %d, want original %d", nb, seedPaise-60000)
	}
}

// ---- Edge cases and authorization ---------------------------------------

func TestTransferEdgeCases(t *testing.T) {
	alice, aTok := signup(t)
	bob, _ := signup(t)

	if r := transfer(t, aTok, bob, seedPaise+1, "over"); r.status != http.StatusUnprocessableEntity {
		t.Errorf("insufficient funds: %d, want 422", r.status)
	}
	if r := transfer(t, aTok, alice, 100, "self"); r.status != http.StatusBadRequest {
		t.Errorf("self transfer: %d, want 400", r.status)
	}
	if r := transfer(t, aTok, "ghost_user_404", 100, "ghost"); r.status != http.StatusNotFound {
		t.Errorf("unknown recipient: %d, want 404", r.status)
	}
	if r := transfer(t, aTok, bob, 0, "zero"); r.status != http.StatusBadRequest {
		t.Errorf("zero amount: %d, want 400", r.status)
	}
	if r := transfer(t, aTok, bob, -5, "neg"); r.status != http.StatusBadRequest {
		t.Errorf("negative amount: %d, want 400", r.status)
	}
	// A rejected transfer must not claim the key: retrying "over" after a
	// top-up would be legitimate, so it must still be 422 now (not replay).
	if r := transfer(t, aTok, bob, seedPaise+1, "over"); r.status != http.StatusUnprocessableEntity {
		t.Errorf("retry of rejected transfer: %d, want 422 again", r.status)
	}
}

func TestTransferReadAuthorization(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)
	_, cTok := signup(t) // uninvolved third party

	created := transfer(t, aTok, bob, 100, "read-auth")
	id := created.body["transfer_id"].(string)

	if r := call(t, "GET", "/transfers/"+id, aTok, nil); r.status != http.StatusOK {
		t.Errorf("sender read: %d, want 200", r.status)
	}
	if r := call(t, "GET", "/transfers/"+id, bTok, nil); r.status != http.StatusOK {
		t.Errorf("recipient read: %d, want 200", r.status)
	}
	if r := call(t, "GET", "/transfers/"+id, cTok, nil); r.status != http.StatusNotFound {
		t.Errorf("third-party read: %d, want 404 (no existence leak)", r.status)
	}
	if r := call(t, "GET", "/transfers/not-a-uuid", aTok, nil); r.status != http.StatusNotFound {
		t.Errorf("malformed id: %d, want 404", r.status)
	}
}

func TestAuthRequired(t *testing.T) {
	for _, path := range []string{"/accounts", "/transfers"} {
		if r := call(t, "POST", path, "", map[string]string{}); r.status != http.StatusUnauthorized {
			t.Errorf("POST %s without token: %d, want 401", path, r.status)
		}
	}
	if r := call(t, "GET", "/accounts/me", "garbage-token", nil); r.status != http.StatusUnauthorized {
		t.Errorf("garbage token: %d, want 401", r.status)
	}
}

func TestAccountBeforeWallet(t *testing.T) {
	_, tok := signup(t)
	if r := call(t, "GET", "/accounts/me", tok, nil); r.status != http.StatusNotFound {
		t.Errorf("GET /accounts/me before wallet: %d, want 404", r.status)
	}
}

// ---- Claim-back (24h transfer reversal) ----------------------------------

func TestClaimBackHappyPath(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)

	created := transfer(t, aTok, bob, 500, "claim-happy")
	if created.status != http.StatusCreated {
		t.Fatalf("transfer: %d (%v)", created.status, created.body)
	}
	id := created.body["transfer_id"].(string)

	r := claim(t, aTok, id)
	if r.status != http.StatusOK {
		t.Fatalf("claim: %d (%v)", r.status, r.body)
	}
	if r.body["status"] != "reversed" {
		t.Errorf("claim status = %v, want reversed", r.body["status"])
	}
	if nb := int64(r.body["new_balance"].(float64)); nb != seedPaise {
		t.Errorf("sender new_balance = %d, want %d", nb, seedPaise)
	}
	if a, b := balance(t, aTok), balance(t, bTok); a != seedPaise || b != seedPaise {
		t.Errorf("balances after claim: sender=%d recipient=%d, want %d each", a, b, seedPaise)
	}
	// The hold disappears with the reversal: recipient fully spendable again.
	if av := availableBalance(t, bTok); av != seedPaise {
		t.Errorf("recipient available = %d, want %d", av, seedPaise)
	}
	// The transfer now reads as reversed.
	get := call(t, "GET", "/transfers/"+id, aTok, nil)
	if get.body["status"] != "reversed" || get.body["reversed_at"] == nil {
		t.Errorf("GET transfer after claim: %v", get.body)
	}
}

// Many concurrent claims of the SAME transfer: the reversal applies exactly
// once, every caller gets the same outcome, balances move once.
func TestClaimBackExactlyOnceConcurrent(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)

	const n, amount = 30, 700
	created := transfer(t, aTok, bob, amount, "claim-race")
	if created.status != http.StatusCreated {
		t.Fatalf("transfer: %d (%v)", created.status, created.body)
	}
	id := created.body["transfer_id"].(string)

	results := runConcurrent(n, func(int) resp {
		return claim(t, aTok, id)
	})

	no5xx(t, results, "concurrent claims")
	var applied, replayed int
	reversedAts := map[any]bool{}
	for _, r := range results {
		if r.status != http.StatusOK {
			t.Errorf("claim status = %d (%v), want 200", r.status, r.body)
			continue
		}
		if r.header.Get("Idempotent-Replay") == "true" {
			replayed++
		} else {
			applied++
		}
		reversedAts[r.body["reversed_at"]] = true
		if nb := int64(r.body["new_balance"].(float64)); nb != seedPaise {
			t.Errorf("new_balance = %d, want %d", nb, seedPaise)
		}
	}
	if applied != 1 {
		t.Errorf("applied = %d, want exactly 1 non-replayed claim", applied)
	}
	if replayed != n-1 {
		t.Errorf("replayed = %d, want %d", replayed, n-1)
	}
	if len(reversedAts) != 1 {
		t.Errorf("distinct reversed_at values = %d, want 1", len(reversedAts))
	}
	if a, b := balance(t, aTok), balance(t, bTok); a != seedPaise || b != seedPaise {
		t.Errorf("balances after claim race: sender=%d recipient=%d, want %d each", a, b, seedPaise)
	}
}

// Received money is on hold while claimable: the recipient can spend their
// own funds but not the held amount — which is exactly what guarantees the
// later claim-back can never fail.
func TestHoldBlocksSpendingHeldFunds(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)
	carol, _ := signup(t)

	created := transfer(t, aTok, bob, 500, "hold-in")
	if created.status != http.StatusCreated {
		t.Fatalf("transfer: %d (%v)", created.status, created.body)
	}
	id := created.body["transfer_id"].(string)

	if bal, av := balance(t, bTok), availableBalance(t, bTok); bal != seedPaise+500 || av != seedPaise {
		t.Fatalf("recipient balance/available = %d/%d, want %d/%d", bal, av, seedPaise+500, seedPaise)
	}
	// One paisa into the held amount must be rejected...
	if r := transfer(t, bTok, carol, seedPaise+1, "spend-held"); r.status != http.StatusUnprocessableEntity {
		t.Errorf("spending held funds: %d (%v), want 422", r.status, r.body)
	}
	// ...but everything up to the hold is spendable.
	if r := transfer(t, bTok, carol, seedPaise, "spend-own"); r.status != http.StatusCreated {
		t.Errorf("spending own funds: %d (%v), want 201", r.status, r.body)
	}

	// Bob's balance is now exactly the held 500 — the claim must still work.
	if r := claim(t, aTok, id); r.status != http.StatusOK {
		t.Errorf("claim after recipient drained own funds: %d (%v)", r.status, r.body)
	}
	if a, b := balance(t, aTok), balance(t, bTok); a != seedPaise || b != 0 {
		t.Errorf("balances after claim: sender=%d recipient=%d, want %d/0", a, b, seedPaise)
	}
}

func TestClaimWindowExpired(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)

	created := transfer(t, aTok, bob, 300, "claim-old")
	id := created.body["transfer_id"].(string)
	backdateTransfer(t, id, 25*time.Hour)

	r := claim(t, aTok, id)
	if r.status != http.StatusConflict {
		t.Errorf("expired claim: %d (%v), want 409", r.status, r.body)
	}
	if r.body["error"] != "claim_window_expired" {
		t.Errorf("expired claim error = %v, want claim_window_expired", r.body["error"])
	}
	// The hold expired with the window: money is the recipient's for keeps.
	if bal, av := balance(t, bTok), availableBalance(t, bTok); bal != seedPaise+300 || av != bal {
		t.Errorf("recipient balance/available = %d/%d, want both %d", bal, av, seedPaise+300)
	}
}

func TestClaimAuthorization(t *testing.T) {
	_, aTok := signup(t)
	bob, bTok := signup(t)
	_, cTok := signup(t) // uninvolved third party

	created := transfer(t, aTok, bob, 100, "claim-auth")
	id := created.body["transfer_id"].(string)

	if r := claim(t, bTok, id); r.status != http.StatusNotFound {
		t.Errorf("recipient claim: %d, want 404 (only the sender may claim)", r.status)
	}
	if r := claim(t, cTok, id); r.status != http.StatusNotFound {
		t.Errorf("third-party claim: %d, want 404 (no existence leak)", r.status)
	}
	if r := claim(t, aTok, "not-a-uuid"); r.status != http.StatusNotFound {
		t.Errorf("malformed id: %d, want 404", r.status)
	}
	// Failed attempts must not burn the sender's claim.
	if r := claim(t, aTok, id); r.status != http.StatusOK {
		t.Errorf("sender claim after rejected attempts: %d, want 200", r.status)
	}
}

// Money returned by a reversal carries no hold — a reversal is not a
// transfer, so the sender can spend it immediately.
func TestClaimedBackMoneySpendableImmediately(t *testing.T) {
	_, aTok := signup(t)
	bob, _ := signup(t)

	created := transfer(t, aTok, bob, 400, "claim-respend")
	id := created.body["transfer_id"].(string)
	if r := claim(t, aTok, id); r.status != http.StatusOK {
		t.Fatalf("claim: %d (%v)", r.status, r.body)
	}
	if av := availableBalance(t, aTok); av != seedPaise {
		t.Errorf("sender available after claim = %d, want %d", av, seedPaise)
	}
	if r := transfer(t, aTok, bob, seedPaise, "respend-all"); r.status != http.StatusCreated {
		t.Errorf("spending claimed-back money: %d (%v), want 201", r.status, r.body)
	}
}

func TestHealthEndpoints(t *testing.T) {
	if r := call(t, "GET", "/healthz", "", nil); r.status != http.StatusOK {
		t.Errorf("healthz: %d", r.status)
	}
	if r := call(t, "GET", "/readyz", "", nil); r.status != http.StatusOK {
		t.Errorf("readyz: %d", r.status)
	}
}
