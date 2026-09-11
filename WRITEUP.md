# Write-up — Wallet / P2P Transfer Service

**Live:** `<RENDER_URL>` · **Repo:** `<GITHUB_URL>` · **Logs:** `<RENDER_URL>/logs` · **Metrics:** `<RENDER_URL>/metrics` · **Gate:** `./scripts/burst.sh <RENDER_URL>`

## Data model

Three tables (Postgres, migrations embedded and applied at startup):
`users(id uuid pk, username unique, password_hash)` ·
`wallets(user_id pk → users, balance_paise bigint CHECK >= 0)` ·
`transfers(id uuid pk, sender_id, recipient_id, amount_paise bigint CHECK > 0, idempotency_key, body_hash, sender_balance_after, UNIQUE(sender_id, idempotency_key))`.
Money is integer paise everywhere; the CHECK constraints are a last-line net behind the application logic. `sender_balance_after` is stored so a replay returns the *original* `new_balance`.

## Get-or-create + transfer under concurrency

Every check-then-act race is resolved by making the database do the check and the act atomically:

- **Wallet get-or-create:** `INSERT … ON CONFLICT (user_id) DO NOTHING`. Two concurrent first-transfers to a brand-new user cannot 500 or double-create: the loser's insert waits on the winner's uncommitted row, then no-ops. A cheap SELECT beforehand lets us *log* the request that lost the race (visible in `/logs` as `get_or_create_race_lost`) without affecting correctness.
- **The transfer** runs in **one transaction**: upsert both wallets, `SELECT … FOR UPDATE` both wallet rows, insert the transfer row (which claims the idempotency key), check funds, update both balances, commit. All row acquisition — upserts and locks — happens in **ascending user_id order**, so concurrent A→B and B→A crossfire cannot deadlock. `FOR UPDATE` was chosen over a guarded `UPDATE … WHERE balance >= x` because we're already multi-statement, and holding the balance lets us distinguish error cases and return exact balances.
- **Rejected heavier alternatives:** app-level mutexes (worthless across replicas), Postgres advisory locks (a hand-rolled lock protocol to re-derive what the unique index already guarantees), SERIALIZABLE + retry loops (correct but needs retry machinery everywhere), Redis/distributed locks (a second infrastructure dependency with its own failure modes), queue serialization (global bottleneck). The upsert + row locks are the smallest mechanism that is actually correct.

## Idempotency

The key lives **on the transfers row** with `UNIQUE(sender_id, idempotency_key)`, inserted **inside the money-moving transaction** — so the key exists *iff* the funds moved; there is no window where one is true without the other. The insert uses `ON CONFLICT DO NOTHING`; on conflict we roll back and re-read the winner's row: matching `body_hash` (sha256 of `to_user|amount_paise`) → **200** replay of the stored outcome with `Idempotent-Replay: true`; different hash → **409**. The claim happens *before* the funds check, so a replay returns its original success even if the balance has since dropped (covered by a test). Rejections (422 etc.) deliberately don't claim the key — they moved no money, so a later retry after a top-up may legitimately succeed. Keys are scoped per sender and kept indefinitely at this scale; in production I'd prune rows older than the client retry horizon (e.g. 30 days) with a scheduled delete.

## Identity & authorization

Signup/login (bcrypt) issue an HS256 JWT (24 h, secret from env). Middleware verifies the signature and puts `sub` → caller-id into the request context; **every** handler takes the caller exclusively from there — `from` never appears in any request. A transfer can only debit the caller's wallet; `GET /transfers/{id}` returns **404** (not 403) to non-participants so IDs don't leak existence.

## Consistency vs. availability

**Writes: consistency, fail closed.** A double-spend is unrecoverable; a rejected request is just a retry. If Postgres is slow or down, a 10 s request deadline cancels in-flight queries and the API returns **503** — never a partial write (single transaction), never a queued "we'll apply it later". **Reads also hit Postgres**: a stale balance is tolerable in principle, but a cache is complexity this scale doesn't need, and `/readyz` (which pings the DB) takes the instance out of rotation during an outage anyway. Priorities: correctness > availability > latency — for money, an honest 503 beats a fast wrong answer.

## Edge cases

Insufficient funds → **422** (well-formed, authorized, but unprocessable against current state — vs 400 "malformed" and 409, reserved for idempotency conflicts); self-transfer → 400; unknown recipient → 404 (users must exist; only their *wallet* is auto-created); zero/negative amount → 400 (typed as int64; floats rejected by strict JSON decoding); replay → 200 + original body; same-key-different-body → 409; missing/invalid token → 401. New wallets are seeded with a configurable ₹1000 so the graders' brand-new pair can transfer — conservation then reads: sum(balances) = wallets_created × seed, which the burst script asserts.

## Container / deploy / observability

Multi-stage Dockerfile: static Go build → **distroless non-root** (~10 MB); `HEALTHCHECK` runs the binary's own `--healthcheck` flag (no shell in distroless). `docker compose up --build` starts app + Postgres 16 in one command. Config is env-only; nothing sensitive committed. Deployed as a **Docker image on Render free tier** + **Neon free Postgres** (TLS). Logs are structured JSON with a per-request `request_id`, naming the meaningful events (`transfer_applied`, `insufficient_funds_rejected`, `idempotent_replay`, `get_or_create_race_lost`, `auth_failure`); the last 1000 events are publicly viewable at **`/logs`** (a deliberate, zero-dependency answer to "public logs link" — full history in Render's dashboard). **`/metrics`** exposes Prometheus counters and a latency histogram (p99 derivable), transfers applied/rejected by reason, replays, races lost, auth failures.

## Verification

Unit tests plus an integration suite (`go test -tags integration ./test/`) that boots the real stack against a real Postgres (embedded automatically if `DATABASE_URL` is unset) and reproduces the gate: bidirectional concurrent first-transfers, 50 concurrent same-key retries, conservation under a 100-transfer random burst, replay-after-balance-drop, and the authorization matrix. `scripts/burst.sh` runs the same gate against any URL.

## AI usage & cost

Built with Claude (Anthropic) as pair-programmer. **I decided:** the mechanism choices above — `FOR UPDATE` over guarded UPDATE, key-inside-transaction, upsert over locks, fail-closed CAP stance, 404-over-403, 422 for insufficient funds, seed-balance approach, stack/host choices. **AI executed under direction:** Go implementation, test suite, Dockerfile/compose, this document's drafting; I reviewed the transfer transaction and tests line-by-line. **Cost: ₹0** — Render free web service, Neon free Postgres, free local toolchain; no card attached anywhere.
