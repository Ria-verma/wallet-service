# wallet-service

A small wallet / peer-to-peer transfer service that never loses money.
Users hold a balance in **integer paise** and transfer to each other; the
design is built around three invariants — conservation, idempotent retries,
and race-free wallet get-or-create — all enforced inside single Postgres
transactions. See [WRITEUP.md](WRITEUP.md) for the design reasoning.

## Run it

```bash
docker compose up --build        # app on :8080, Postgres 16, one command
```

Or locally: `DATABASE_URL=... JWT_SECRET=... go run ./cmd/server`
(config via env only — see `.env.example`).

## API

| Route | Auth | Purpose |
|---|---|---|
| `POST /auth/signup` `{username, password}` | – | create user → `{token, user_id}` |
| `POST /auth/login` `{username, password}` | – | → `{token}` |
| `POST /accounts` | Bearer | get-or-create my wallet → `{user_id, balance_paise}` |
| `GET /accounts/me` | Bearer | → `{balance_paise}` |
| `POST /transfers` `{to_user, amount_paise, idempotency_key}` | Bearer | move funds → `{transfer_id, new_balance}` |
| `GET /transfers/{id}` | Bearer | details, participants only |
| `GET /healthz` / `GET /readyz` | – | liveness / readiness (incl. DB ping) |
| `GET /metrics` / `GET /logs` | – | Prometheus metrics / recent structured logs |

New wallets are seeded with `INITIAL_BALANCE_PAISE` (default ₹1000) so a
fresh pair of users can transfer immediately — a demo affordance, stated in
the write-up.

```bash
BASE=http://localhost:8080
TOKEN=$(curl -s $BASE/auth/signup -d '{"username":"alice","password":"password123"}' | jq -r .token)
curl -s $BASE/auth/signup -d '{"username":"bob","password":"password123"}' > /dev/null
curl -s -X POST $BASE/transfers -H "Authorization: Bearer $TOKEN" \
  -d '{"to_user":"bob","amount_paise":500,"idempotency_key":"demo-1"}'
# retry with the same key → 200, same transfer_id, header Idempotent-Replay: true
```

Transfer responses: `201` applied · `200` idempotent replay ·
`400` validation/self-transfer · `404` unknown recipient · `409` same key
different body · `422` insufficient funds · `503` database unavailable
(fail closed — safe to retry).

## The correctness gate

```bash
./scripts/burst.sh <base-url>    # needs curl + jq
```

Creates two brand-new users, fires 30 concurrent **first** transfers in both
directions (wallet get-or-create race + A↔B lock crossfire), then 30
concurrent retries sharing **one** idempotency key, and asserts: zero 5xx,
every wallet created exactly once, money moved exactly once for the shared
key, and both balances reconcile to the paisa.

## Tests

```bash
go test ./...                        # unit
go test -tags integration ./test/    # full stack vs real Postgres
```

Integration tests use `DATABASE_URL` if set (e.g. the compose db on
`localhost:5433`); otherwise they start an embedded Postgres automatically —
no setup needed. They reproduce the burst gate in-process: bidirectional
first-transfer races, 50 same-key retries, conservation under a 100-transfer
random burst, replay-after-balance-drop, and the full edge-case matrix.

## Operations

- **Image**: multi-stage build → distroless static, non-root, ~10 MB;
  `HEALTHCHECK` runs the binary's own `--healthcheck` probe.
- **Migrations**: embedded in the binary, applied at startup.
- **Logs**: structured JSON on stdout with a `request_id` per request;
  the last 1000 events are publicly viewable at `GET /logs`.
- **Metrics**: `GET /metrics` — request counts, latency histogram (p99),
  transfers applied/rejected by reason, idempotent replays, get-or-create
  races lost, auth failures.
- **Config** (env only): `DATABASE_URL`, `JWT_SECRET`, `PORT`,
  `INITIAL_BALANCE_PAISE`.

Deployed at **https://wallet-service-irfj.onrender.com** — Render free tier
(Docker runtime, Ohio, co-located with the database) + Neon free Postgres.
Note: the free instance sleeps when idle — the first request after a quiet
period takes ~1 min to cold-start.
