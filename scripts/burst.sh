#!/usr/bin/env bash
# The correctness gate: two brand-new users, a concurrent burst of
# first-transfers in both directions (wallet get-or-create race + deadlock
# crossfire), then a concurrent burst of retries sharing ONE idempotency
# key. Asserts: no 5xx, wallets created exactly once, retries apply once,
# balances reconcile to the paisa.
#
# Usage:  ./scripts/burst.sh https://your-app.onrender.com
set -euo pipefail

BASE="${1:?usage: burst.sh <base-url>   e.g. burst.sh http://localhost:8080}"
command -v jq >/dev/null || { echo "ERROR: jq is required (brew install jq)"; exit 1; }

N_FIRST=15      # first-transfers per direction (2x this total, concurrent)
AMT=100         # paise per first-transfer
RETRIES=30      # concurrent retries sharing one idempotency key
RETRY_AMT=250   # paise for the shared-key transfer

SUF="$(date +%s)$RANDOM"
ALICE="alice_$SUF"; BOB="bob_$SUF"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
FAILED=0

fail() { echo "  ✗ $*"; FAILED=1; }
pass() { echo "  ✓ $*"; }

signup() {
  curl -sS -X POST "$BASE/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"burst-pass-123\"}" | jq -r .token
}

balance() {
  curl -sS "$BASE/accounts/me" -H "Authorization: Bearer $1" | jq -r .balance_paise
}

# xfer TOKEN TO AMOUNT KEY OUTFILE — status code to .code, body to .body
xfer() {
  curl -sS -o "$5.body" -w '%{http_code}\n' -X POST "$BASE/transfers" \
    -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "{\"to_user\":\"$2\",\"amount_paise\":$3,\"idempotency_key\":\"$4\"}" > "$5.code"
}
export -f xfer
export BASE

echo "== Setup: two brand-new users =="
TOK_A="$(signup "$ALICE")"; TOK_B="$(signup "$BOB")"
[ "$TOK_A" != "null" ] && [ -n "$TOK_A" ] || { echo "signup failed for $ALICE"; exit 1; }
[ "$TOK_B" != "null" ] && [ -n "$TOK_B" ] || { echo "signup failed for $BOB"; exit 1; }
pass "users $ALICE and $BOB created (no wallets yet)"

echo
echo "== Phase A: $((2*N_FIRST)) concurrent FIRST transfers, both directions =="
{
  for i in $(seq 1 "$N_FIRST"); do printf '%s\t%s\t%s\t%s\t%s\n' "$TOK_A" "$BOB"   "$AMT" "a2b-$SUF-$i" "$TMP/a_$i"; done
  for i in $(seq 1 "$N_FIRST"); do printf '%s\t%s\t%s\t%s\t%s\n' "$TOK_B" "$ALICE" "$AMT" "b2a-$SUF-$i" "$TMP/b_$i"; done
} | xargs -P 30 -n 5 bash -c 'xfer "$1" "$2" "$3" "$4" "$5"' _

CODES_A="$(cat "$TMP"/{a,b}_*.code)"
N5XX="$(grep -c '^5' <<<"$CODES_A" || true)"
N201="$(grep -c '^201$' <<<"$CODES_A" || true)"
[ "$N5XX" -eq 0 ] && pass "zero 5xx under the first-transfer burst" || fail "$N5XX responses were 5xx"
[ "$N201" -eq $((2*N_FIRST)) ] && pass "all $((2*N_FIRST)) transfers applied (201)" || fail "only $N201/$((2*N_FIRST)) got 201"

BAL_A1="$(balance "$TOK_A")"; BAL_B1="$(balance "$TOK_B")"
SUM1=$((BAL_A1 + BAL_B1))
[ "$BAL_A1" -eq "$BAL_B1" ] \
  && pass "symmetric crossfire nets to zero (both balances: $BAL_A1)" \
  || fail "balances diverged after symmetric traffic: $BAL_A1 vs $BAL_B1"

echo
echo "== Phase B: $RETRIES concurrent retries, SAME idempotency key =="
KEY="shared-$SUF"
for i in $(seq 1 "$RETRIES"); do printf '%s\t%s\t%s\t%s\t%s\n' "$TOK_A" "$BOB" "$RETRY_AMT" "$KEY" "$TMP/r_$i"; done \
  | xargs -P 30 -n 5 bash -c 'xfer "$1" "$2" "$3" "$4" "$5"' _

CODES_B="$(cat "$TMP"/r_*.code)"
R5XX="$(grep -c '^5' <<<"$CODES_B" || true)"
R201="$(grep -c '^201$' <<<"$CODES_B" || true)"
R200="$(grep -c '^200$' <<<"$CODES_B" || true)"
UNIQUE_IDS="$(cat "$TMP"/r_*.body | jq -r .transfer_id | grep -v '^null$' | sort -u | wc -l | tr -d ' ')"

[ "$R5XX" -eq 0 ] && pass "zero 5xx under the same-key retry burst" || fail "$R5XX retry responses were 5xx"
[ "$R201" -eq 1 ] && pass "money moved exactly once (one 201)" || fail "$R201 responses were 201, want exactly 1"
[ "$R200" -eq $((RETRIES-1)) ] && pass "$R200 retries replayed the original outcome (200)" || fail "$R200 replays, want $((RETRIES-1))"
[ "$UNIQUE_IDS" -eq 1 ] && pass "every response carries the same transfer_id" || fail "$UNIQUE_IDS distinct transfer_ids, want 1"

echo
echo "== Reconciliation =="
BAL_A2="$(balance "$TOK_A")"; BAL_B2="$(balance "$TOK_B")"
SUM2=$((BAL_A2 + BAL_B2))
[ "$SUM1" -eq "$SUM2" ] && pass "total money conserved ($SUM1 paise before and after)" || fail "conservation violated: $SUM1 -> $SUM2"
[ "$BAL_A2" -eq $((BAL_A1 - RETRY_AMT)) ] && pass "sender debited exactly once (-$RETRY_AMT)" || fail "sender balance $BAL_A2, want $((BAL_A1 - RETRY_AMT))"
[ "$BAL_B2" -eq $((BAL_B1 + RETRY_AMT)) ] && pass "recipient credited exactly once (+$RETRY_AMT)" || fail "recipient balance $BAL_B2, want $((BAL_B1 + RETRY_AMT))"

# Same key + different body must be a 409, and only participants may read.
CONFLICT="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/transfers" \
  -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' \
  -d "{\"to_user\":\"$BOB\",\"amount_paise\":999,\"idempotency_key\":\"$KEY\"}")"
[ "$CONFLICT" = "409" ] && pass "same key + different body -> 409" || fail "same key + different body -> $CONFLICT, want 409"

TID="$(cat "$TMP"/r_1.body | jq -r .transfer_id)"
READ_OK="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/transfers/$TID" -H "Authorization: Bearer $TOK_B")"
[ "$READ_OK" = "200" ] && pass "participant can read the transfer" || fail "participant read -> $READ_OK"

echo
if [ "$FAILED" -eq 0 ]; then
  echo "RESULT: PASS — wallets created once, retries applied once, money conserved."
else
  echo "RESULT: FAIL — see ✗ lines above."
  exit 1
fi
