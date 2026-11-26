package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnnieAnyWay/banking-api/internal/auth"
	"github.com/AnnieAnyWay/banking-api/internal/docs"
	"github.com/AnnieAnyWay/banking-api/internal/ratelimit"
	"github.com/AnnieAnyWay/banking-api/internal/store"
)

type client struct {
	t     *testing.T
	srv   *httptest.Server
	token string
}

func newServer(t *testing.T, limiter *ratelimit.Limiter) *httptest.Server {
	t.Helper()
	log.SetOutput(io.Discard)
	srv := httptest.NewServer(New(Deps{
		Store:   store.New(),
		Users:   auth.NewUsers(1000), // fast hashing for tests
		Tokens:  auth.NewTokens([]byte("test-secret-test-secret-test-secret"), time.Hour),
		Limiter: limiter,
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *client) do(method, path, body string) (int, map[string]any) {
	c.t.Helper()
	code, out, _ := c.raw(method, path, body)
	return code, out
}

func (c *client) raw(method, path, body string) (int, map[string]any, http.Header) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, res.Header
}

// login registers a user and returns an authenticated client.
func login(t *testing.T, srv *httptest.Server, user string) *client {
	t.Helper()
	c := &client{t: t, srv: srv}
	creds := `{"username":"` + user + `","password":"password123"}`
	if code, res := c.do("POST", "/auth/register", creds); code != 201 {
		t.Fatalf("register: %d %v", code, res)
	}
	code, res := c.do("POST", "/auth/login", creds)
	if code != 200 {
		t.Fatalf("login: %d %v", code, res)
	}
	c.token = res["accessToken"].(string)
	return c
}

func (c *client) account() string {
	c.t.Helper()
	code, res := c.do("POST", "/accounts", `{"name":"main"}`)
	if code != 201 {
		c.t.Fatalf("create account: %d %v", code, res)
	}
	return res["id"].(string)
}

func tx(from, to, amount, cur, typ string) string {
	m := map[string]any{"amount": json.RawMessage(amount), "currency": cur, "type": typ}
	if from != "" {
		m["fromAccount"] = from
	}
	if to != "" {
		m["toAccount"] = to
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestAuthFlow(t *testing.T) {
	srv := newServer(t, nil)
	anon := &client{t: t, srv: srv}

	if code, _ := anon.do("GET", "/transactions", ""); code != 401 {
		t.Fatalf("no token: got %d, want 401", code)
	}
	anon.token = "not.a.jwt"
	if code, _ := anon.do("GET", "/transactions", ""); code != 401 {
		t.Fatalf("bad token: got %d, want 401", code)
	}
	anon.token = ""

	c := login(t, srv, "anna")
	if code, _ := anon.do("POST", "/auth/register", `{"username":"ANNA","password":"password123"}`); code != 409 {
		t.Fatalf("duplicate (case-insensitive) user: got %d, want 409", code)
	}
	if code, _ := anon.do("POST", "/auth/login", `{"username":"anna","password":"wrong-password"}`); code != 401 {
		t.Fatalf("wrong password: got %d, want 401", code)
	}
	if code, _ := anon.do("POST", "/auth/login", `{"username":"nobody","password":"password123"}`); code != 401 {
		t.Fatalf("unknown user: got %d, want 401", code)
	}
	code, res := anon.do("POST", "/auth/register", `{"username":"x","password":"short"}`)
	if code != 400 || len(res["details"].([]any)) != 2 {
		t.Fatalf("weak credentials: %d %v", code, res)
	}

	acc := c.account()
	code, me := c.do("GET", "/auth/me", "")
	if code != 200 || me["username"] != "anna" || len(me["accounts"].([]any)) != 1 {
		t.Fatalf("me: %d %v", code, me)
	}
	if !strings.HasPrefix(acc, "ACC-") || len(acc) != 9 {
		t.Fatalf("account id %q", acc)
	}
}

func TestOwnership(t *testing.T) {
	srv := newServer(t, nil)
	alice, bob := login(t, srv, "alice"), login(t, srv, "bobby")
	a, b := alice.account(), bob.account()

	if code, _ := alice.do("POST", "/transactions", tx("", a, "100", "USD", "deposit")); code != 201 {
		t.Fatal("alice deposit to own account")
	}
	if code, _ := alice.do("POST", "/transactions", tx("", b, "100", "USD", "deposit")); code != 403 {
		t.Fatalf("deposit into someone else's account: got %d, want 403", code)
	}
	if code, _ := bob.do("POST", "/transactions", tx(a, b, "10", "USD", "transfer")); code != 403 {
		t.Fatalf("transfer from someone else's account: got %d, want 403", code)
	}
	code, tr := alice.do("POST", "/transactions", tx(a, b, "10", "USD", "transfer"))
	if code != 201 || tr["status"] != "completed" {
		t.Fatalf("transfer to another user: %d %v", code, tr)
	}
	if code, _ := alice.do("POST", "/transactions", tx(a, "ACC-ZZZZZ", "1", "USD", "transfer")); code != 404 {
		t.Fatalf("transfer to unknown account: got %d, want 404", code)
	}
	if code, _ := bob.do("GET", "/accounts/"+a+"/balance", ""); code != 403 {
		t.Fatalf("other user's balance: got %d, want 403", code)
	}
	if code, _ := bob.do("GET", "/accounts/"+a+"/summary", ""); code != 403 {
		t.Fatalf("other user's summary: got %d, want 403", code)
	}
	if code, _ := bob.do("GET", "/transactions?accountId="+a, ""); code != 403 {
		t.Fatalf("filter on other user's account: got %d, want 403", code)
	}
	// Bob sees the incoming transfer, but not Alice's deposit.
	_, list := bob.do("GET", "/transactions", "")
	if list["count"] != 1.0 {
		t.Fatalf("bob sees %v transactions, want 1", list["count"])
	}
	_, dep := alice.do("GET", "/transactions?type=deposit", "")
	depID := dep["transactions"].([]any)[0].(map[string]any)["id"].(string)
	if code, _ := bob.do("GET", "/transactions/"+depID, ""); code != 404 {
		t.Fatalf("other user's transaction by id: got %d, want 404", code)
	}
	_, bal := bob.do("GET", "/accounts/"+b+"/balance", "")
	if bal["balances"].(map[string]any)["USD"] != 10.0 {
		t.Fatalf("bob balance %v", bal)
	}
}

func TestTransactionFlow(t *testing.T) {
	srv := newServer(t, nil)
	c := login(t, srv, "anna")
	a1, a2 := c.account(), c.account()

	code, dep := c.do("POST", "/transactions", tx("", a1, "500", "USD", "deposit"))
	if code != 201 || dep["status"] != "completed" {
		t.Fatalf("deposit: %d %v", code, dep)
	}
	code, tr := c.do("POST", "/transactions", tx(a1, a2, "100.50", "USD", "transfer"))
	if code != 201 || tr["status"] != "completed" || tr["amount"] != 100.5 {
		t.Fatalf("transfer: %d %v", code, tr)
	}
	code, fail := c.do("POST", "/transactions", tx(a2, "", "1000", "USD", "withdrawal"))
	if code != 201 || fail["status"] != "failed" {
		t.Fatalf("overdraft should fail: %d %v", code, fail)
	}

	_, bal := c.do("GET", "/accounts/"+a1+"/balance", "")
	if got := bal["balances"].(map[string]any)["USD"]; got != 399.5 {
		t.Fatalf("balance = %v, want 399.5", got)
	}
	_, bal = c.do("GET", "/accounts/"+a2+"/balance", "")
	if got := bal["balances"].(map[string]any)["USD"]; got != 100.5 {
		t.Fatalf("balance = %v, want 100.5", got)
	}
	code, got := c.do("GET", "/transactions/"+tr["id"].(string), "")
	if code != 200 || got["id"] != tr["id"] {
		t.Fatalf("get by id: %d %v", code, got)
	}
	if code, _ := c.do("GET", "/transactions/nope", ""); code != 404 {
		t.Fatalf("missing tx: %d", code)
	}
	if code, _ := c.do("GET", "/accounts/ACC-00000/balance", ""); code != 404 {
		t.Fatalf("unknown account: %d", code)
	}
	_, sum := c.do("GET", "/accounts/"+a1+"/summary", "")
	usd := sum["byCurrency"].(map[string]any)["USD"].(map[string]any)
	if sum["transactionCount"] != 2.0 || usd["totalDeposits"] != 500.0 || usd["totalTransfersOut"] != 100.5 {
		t.Fatalf("summary: %v", sum)
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t, nil)
	c := login(t, srv, "anna")
	a, b := c.account(), c.account()
	c.do("POST", "/transactions", tx("", a, "10", "EUR", "deposit"))
	c.do("POST", "/transactions", tx("", b, "20", "EUR", "deposit"))
	c.do("POST", "/transactions", tx(a, b, "5", "EUR", "transfer"))

	cases := map[string]float64{
		"":                                  3,
		"?accountId=" + a:                   2,
		"?type=transfer":                    1,
		"?accountId=" + b + "&type=deposit": 1,
		"?from=2000-01-01&to=2999-12-31":    3,
		"?from=2999-01-01":                  0,
		"?to=2000-01-01":                    0,
	}
	for q, want := range cases {
		code, body := c.do("GET", "/transactions"+q, "")
		if code != 200 || body["count"] != want {
			t.Errorf("%q: %d count=%v want %v", q, code, body["count"], want)
		}
	}
	for _, q := range []string{"?type=bogus", "?accountId=bad", "?from=yesterday", "?from=2024-02-01&to=2024-01-01"} {
		if code, _ := c.do("GET", "/transactions"+q, ""); code != 400 {
			t.Errorf("%q: got %d, want 400", q, code)
		}
	}
}

func TestValidation(t *testing.T) {
	srv := newServer(t, nil)
	c := login(t, srv, "anna")
	cases := map[string]string{
		`{"toAccount":"ACC-12345","amount":-5,"currency":"USD","type":"deposit"}`:                           "amount",
		`{"toAccount":"ACC-12345","amount":0,"currency":"USD","type":"deposit"}`:                            "amount",
		`{"toAccount":"ACC-12345","amount":1.234,"currency":"USD","type":"deposit"}`:                        "amount",
		`{"toAccount":"ACC-12345","amount":"10","currency":"USD","type":"deposit"}`:                         "amount",
		`{"toAccount":"ACC-12345","amount":1e3,"currency":"USD","type":"deposit"}`:                          "amount",
		`{"toAccount":"ACC-12345","amount":10,"currency":"XYZ","type":"deposit"}`:                           "currency",
		`{"toAccount":"12345","amount":10,"currency":"USD","type":"deposit"}`:                               "toAccount",
		`{"toAccount":"ACC-1234!","amount":10,"currency":"USD","type":"deposit"}`:                           "toAccount",
		`{"toAccount":"ACC-12345","amount":10,"currency":"USD","type":"loan"}`:                              "type",
		`{"amount":10,"currency":"USD","type":"transfer","fromAccount":"ACC-12345"}`:                        "toAccount",
		`{"fromAccount":"ACC-12345","toAccount":"ACC-12345","amount":1,"currency":"USD","type":"transfer"}`: "toAccount",
	}
	for body, field := range cases {
		code, res := c.do("POST", "/transactions", body)
		if code != 400 || res["error"] != "Validation failed" {
			t.Errorf("%s: %d %v", body, code, res)
			continue
		}
		found := false
		for _, d := range res["details"].([]any) {
			if d.(map[string]any)["field"] == field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no error for %s in %v", body, field, res["details"])
		}
	}
	for _, body := range []string{"", "{bad json", `{"unknown":1}`} {
		if code, _ := c.do("POST", "/transactions", body); code != 400 {
			t.Errorf("%q: got %d, want 400", body, code)
		}
	}
}

func TestRateLimit(t *testing.T) {
	srv := newServer(t, ratelimit.New(3, time.Minute))
	c := &client{t: t, srv: srv}
	for i := 0; i < 3; i++ {
		code, _, h := c.raw("GET", "/health", "")
		if code != 200 || h.Get("X-RateLimit-Limit") != "3" {
			t.Fatalf("request %d: %d %v", i, code, h)
		}
	}
	code, res, h := c.raw("GET", "/health", "")
	if code != 429 || h.Get("Retry-After") == "" || h.Get("X-RateLimit-Remaining") != "0" || res["error"] != "Too many requests" {
		t.Fatalf("4th request: %d %v %v", code, res, h)
	}
}

func TestDocs(t *testing.T) {
	srv := newServer(t, nil)
	for path, ctype := range map[string]string{"/openapi.yaml": "application/yaml", "/docs": "text/html"} {
		res, err := http.Get(srv.URL + path)
		if err != nil || res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), ctype) {
			t.Fatalf("%s: %v %v", path, err, res)
		}
		res.Body.Close()
	}
	// Every route must be documented.
	spec := string(docs.Spec())
	for _, p := range []string{"/health:", "/auth/register:", "/auth/login:", "/auth/me:", "/accounts:",
		"/accounts/{accountId}/balance:", "/accounts/{accountId}/summary:", "/transactions:", "/transactions/{id}:"} {
		if !strings.Contains(spec, "\n  "+p) {
			t.Errorf("openapi.yaml is missing path %s", p)
		}
	}
}
