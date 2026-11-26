#!/usr/bin/env bash
# End-to-end demo: register, log in, open accounts, load sample-data.json,
# then call every endpoint, including the auth, ownership and rate-limit errors.
# Start the server first: ./demo/run.sh
set -euo pipefail
BASE="${BASE:-http://localhost:3000}"
DIR="$(dirname "$0")"
pp() { if command -v jq >/dev/null; then jq .; else cat; echo; fi; }
step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
api() { # api <token> <method> <path> [body]
  local tok=$1 m=$2 p=$3; shift 3
  curl -s -X "$m" "$BASE$p" -H 'Content-Type: application/json' ${tok:+-H "Authorization: Bearer $tok"} ${1:+-d "$1"}
}

step "Register + log in users"
tok() { eval "printf %s \"\$TOKEN_$1\""; }   # bash 3.2-friendly lookup
while IFS=$'\t' read -r user pass; do
  creds="{\"username\":\"$user\",\"password\":\"$pass\"}"
  api "" POST /auth/register "$creds" | pp
  token=$(api "" POST /auth/login "$creds" | field accessToken)
  printf -v "TOKEN_$user" %s "$token"
done < <(python3 -c 'import json,sys; [print(u["username"]+"\t"+u["password"]) for u in json.load(open(sys.argv[1]))["users"]]' "$DIR/sample-data.json")
echo "alice token: $(tok alice | cut -c1-40)..."

step "Open accounts"
ALICE=$(api "$(tok alice)" POST /accounts '{"name":"Alice main"}' | tee /dev/stderr | field id)
BOB=$(api "$(tok bob)" POST /accounts '{"name":"Bob main"}' | tee /dev/stderr | field id)

step "Load sample transactions"
while IFS=$'\t' read -r user body; do
  body=${body//\$ALICE/$ALICE}; body=${body//\$BOB/$BOB}
  api "$(tok "$user")" POST /transactions "$body" | pp
done < <(python3 -c '
import json,sys
for t in json.load(open(sys.argv[1]))["transactions"]:
    u=t.pop("as"); print(u+"\t"+json.dumps(t))' "$DIR/sample-data.json")

A=$(tok alice)
step "Who am I";                      api "$A" GET /auth/me | pp
step "Alice: all transactions";       api "$A" GET /transactions | pp
step "Filter: type=transfer";         api "$A" GET "/transactions?type=transfer" | pp
step "Filter: account + date range";  api "$A" GET "/transactions?accountId=$ALICE&from=2024-01-01&to=2099-12-31" | pp
step "Alice balance";                 api "$A" GET "/accounts/$ALICE/balance" | pp
step "Alice summary";                 api "$A" GET "/accounts/$ALICE/summary" | pp
step "Bob balance (received 100.50)"; api "$(tok bob)" GET "/accounts/$BOB/balance" | pp

step "401: no token";                 api "" GET /transactions | pp
step "403: Bob reads Alice's balance"; api "$(tok bob)" GET "/accounts/$ALICE/balance" | pp
step "400: validation error"
api "$A" POST /transactions '{"fromAccount":"12345","toAccount":"ACC-67890","amount":-10.555,"currency":"XYZ","type":"transfer"}' | pp
step "404: unknown transaction";      api "$A" GET /transactions/does-not-exist | pp

if [ "${DEMO_RATE_LIMIT:-0}" = 1 ]; then
  step "429: hammer /health until rate limited"
  for i in $(seq 1 150); do
    code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health")
    if [ "$code" = 429 ]; then echo "request $i -> 429"; curl -si "$BASE/health" | grep -iE '^(HTTP|retry-after|x-ratelimit)'; break; fi
  done
else
  echo; echo "(set DEMO_RATE_LIMIT=1 to also demo the 429 response)"
fi
