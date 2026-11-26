# ▶️ How to Run the application

## Prerequisites
- **Go 1.22+** (built and tested with Go 1.27). macOS: `brew install go`
- `curl` (optional: `jq` for pretty output)

## 1. Start the server
```bash
cd homework-1
./demo/run.sh                 # listens on http://localhost:3000
PORT=8080 ./demo/run.sh       # custom port
JWT_SECRET="$(openssl rand -hex 32)" RATE_LIMIT=200 ./demo/run.sh   # stable tokens, custom limit
```
Or by hand:
```bash
cd homework-1/src
go run ./cmd/server
# or build a binary
go build -o bin/banking-api ./cmd/server && ./bin/banking-api
```
Check that it's up:
```bash
curl http://localhost:3000/health   # {"status":"ok"}
```

## 2. Try the API

### Option A: Swagger UI
Open **http://localhost:3000/docs**.
1. `POST /auth/register` with `{"username":"anna","password":"password123"}`
2. `POST /auth/login` with the same body, then copy `accessToken`
3. Click **Authorize** and paste the token
4. `POST /accounts`, then try deposits, transfers, balances and so on

### Option B: demo script
In a second terminal:
```bash
./demo/sample-requests.sh                     # registers users, opens accounts, loads demo/sample-data.json, calls every endpoint
DEMO_RATE_LIMIT=1 ./demo/sample-requests.sh   # also sends requests until it gets a 429
```
Run it against a freshly started server, because the users already exist after the first run.

### Option C: REST Client
Open `demo/sample-requests.http` in VS Code (REST Client extension) or a JetBrains IDE. The token and account ID are filled in automatically from earlier responses.

### Option D: curl
```bash
curl -s -X POST localhost:3000/auth/register -H 'Content-Type: application/json' -d '{"username":"anna","password":"password123"}'
TOKEN=$(curl -s -X POST localhost:3000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"anna","password":"password123"}' | sed -E 's/.*"accessToken":"([^"]+)".*/\1/')
ACC=$(curl -s -X POST localhost:3000/accounts -H "Authorization: Bearer $TOKEN" | sed -E 's/.*"id":"([^"]+)".*/\1/')
curl -s -X POST localhost:3000/transactions -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"toAccount\":\"$ACC\",\"amount\":100.50,\"currency\":\"USD\",\"type\":\"deposit\"}"
curl -s localhost:3000/accounts/$ACC/balance -H "Authorization: Bearer $TOKEN"
```

## 3. Run the tests
```bash
cd homework-1/src
go test -race -cover ./...
```

Stop the server with `Ctrl+C` (it shuts down gracefully). Data (users, accounts, transactions) is kept in memory only, so it's cleared on restart.
