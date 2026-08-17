#!/usr/bin/env bash
#
# Check that a running caddy-reader behaves the way it is described.
#
#   ./verify.sh                 # a helper on this machine
#   ./verify.sh 10.0.0.2:9776   # one on another machine
#
# The point is not that it works — you will notice that soon enough. The point is
# that the things it CANNOT do are things it actually cannot do: a request with
# no token is refused, a write is refused even holding a valid token, no other
# admin-API path is reachable through it, and the secrets in your Caddy config do
# not cross the network.
#
# Every one of those is a claim in the README. This checks them on your machine
# rather than asking you to believe them.
#
set -uo pipefail
cd "$(dirname "$0")"

TARGET="${1:-localhost:9776}"
ADMIN="${CADDY_ADMIN:-http://127.0.0.1:2019}"

# Token from the environment, or from where the systemd unit keeps it.
TOKEN="${READER_TOKEN:-}"
if [ -z "$TOKEN" ] && [ -r /etc/caddy-reader.env ]; then
  TOKEN=$(grep -E '^READER_TOKEN=' /etc/caddy-reader.env | cut -d= -f2-)
fi
if [ -z "$TOKEN" ]; then
  echo "No token. Set READER_TOKEN, or run this where /etc/caddy-reader.env is readable." >&2
  exit 2
fi

bold=$(tput bold 2>/dev/null || true)
red=$(tput setaf 1 2>/dev/null || true)
green=$(tput setaf 2 2>/dev/null || true)
dim=$(tput setaf 8 2>/dev/null || true)
off=$(tput sgr0 2>/dev/null || true)

pass=0
fail=0

# check <description> <expected> <actual> [explanation shown when it fails]
check() {
  local what="$1" want="$2" got="$3" why="${4:-}"
  if [ "$want" = "$got" ]; then
    printf '  %sPASS%s  %-42s %s\n' "$green" "$off" "$what" "$dim$got$off"
    pass=$((pass + 1))
  else
    printf '  %sFAIL%s  %-42s %sgot %s, expected %s%s\n' "$red" "$off" "$what" "$red" "$got" "$want" "$off"
    [ -n "$why" ] && printf '        %s%s%s\n' "$dim" "$why" "$off"
    fail=$((fail + 1))
  fi
}

code() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@" 2>/dev/null || echo "000"; }

printf '\n%scaddy-reader%s  %s\n\n' "$bold" "$off" "$TARGET"

# --- is it there at all -------------------------------------------------------

health=$(curl -s --max-time 10 "http://$TARGET/health" 2>/dev/null || true)
if [ -z "$health" ]; then
  printf '  %sFAIL%s  nothing is answering on %s\n\n' "$red" "$off" "$TARGET"
  echo "  Is it running?   systemctl status caddy-reader"
  echo "  Right port?      ss -lntp | grep 9776"
  exit 1
fi

case "$health" in
  *'"caddy":"reachable"'*|*'"caddy": "reachable"'*)
    check "helper is up, and can see Caddy" "ok" "ok" ;;
  *)
    check "helper is up, and can see Caddy" "ok" "caddy unreachable" \
      "The helper is running but cannot reach Caddy's admin API at $ADMIN."
    ;;
esac

# --- what it must refuse ------------------------------------------------------

check "a request with no token is refused" \
  "401" "$(code "http://$TARGET/config")" \
  "Anyone on the network could read your proxy layout."

check "a request with the wrong token is refused" \
  "401" "$(code -H 'Authorization: Bearer definitely-not-the-token' "http://$TARGET/config")"

for method in POST PUT PATCH DELETE; do
  check "a $method is refused even with a valid token" \
    "405" "$(code -X "$method" -H "Authorization: Bearer $TOKEN" -d '{}' "http://$TARGET/config")" \
    "This helper must never be able to change Caddy's configuration."
done

# Caddy's admin API has many endpoints. None may be reachable through here,
# because the helper never takes a path from the request.
for path in load id/foo reverse_proxy/upstreams stop; do
  check "admin path /$path is not forwarded" \
    "404" "$(code -H "Authorization: Bearer $TOKEN" "http://$TARGET/$path")"
done

# --- what it must do ----------------------------------------------------------

check "a request with the token is served" \
  "200" "$(code -H "Authorization: Bearer $TOKEN" "http://$TARGET/config")"

body=$(curl -s --max-time 10 -H "Authorization: Bearer $TOKEN" "http://$TARGET/config" 2>/dev/null || true)

case "$body" in
  *'"projected":true'*|*'"projected": true'*)
    check "it is filtering, not forwarding wholesale" "filtered" "filtered" ;;
  *)
    check "it is filtering, not forwarding wholesale" "filtered" "RAW" \
      "This helper runs with --raw and is sending your whole config, secrets included." ;;
esac

domains=$(printf '%s' "$body" | grep -oE '"host":\[[^]]*\]' | grep -oE '"[^"]+\.[^"]+"' | sort -u | wc -l)
printf '  %s——%s    %-42s %s%s domain(s) visible%s\n' "$dim" "$off" "reading Caddy" "$dim" "$domains" "$off"

# --- is it allowed to change anything ----------------------------------------

caddyfile=$(curl -s --max-time 10 -H "Authorization: Bearer $TOKEN" "http://$TARGET/caddyfile" 2>/dev/null || true)

case "$caddyfile" in
  *'"writable":true'*|*'"writable": true'*)
    # Not a failure — it is what the manager needs. But it is a much bigger
    # capability than reading, and it should never be a surprise.
    printf '\n  %s!!%s    %-42s %s%s\n' "$red" "$off" "write access" \
      "${red}ENABLED — this helper can rewrite your Caddyfile$off"
    printf '        %sThat is required for the manager. If you did not mean to, drop --writable.%s\n' "$dim" "$off"
    ;;
  *'"writable"'*)
    printf '\n  %s——%s    %-42s %sread-only; the manager cannot change anything%s\n' \
      "$dim" "$off" "write access" "$dim" "$off"
    ;;
  *)
    printf '\n  %s——%s    %-42s %snot served by this version%s\n' \
      "$dim" "$off" "the Caddyfile endpoint" "$dim" "$off"
    ;;
esac

# --- do the secrets stay home -------------------------------------------------
#
# Only possible where the admin API is reachable — i.e. on the Caddy host itself.
# Elsewhere, skip it honestly rather than pretending to have checked.

raw=$(curl -s --max-time 10 "${ADMIN%/}/config/" 2>/dev/null || true)

if [ -z "$raw" ]; then
  printf '\n  %sskipped%s the secret-leak check: Caddy'"'"'s admin API is not reachable from here.\n' "$dim" "$off"
  printf '  %sRun this on the Caddy host itself to include it.%s\n' "$dim" "$off"
else
  # Shapes that are credentials wherever they appear in a Caddy config: bcrypt
  # and argon2 hashes from basic_auth, and ACME external-account binding keys.
  markers='\$2[aby]\$|\$argon2|"mac_key"|"key_id"'
  raw_hits=$(printf '%s' "$raw" | grep -oE "$markers" | wc -l | tr -d ' ')
  sent_hits=$(printf '%s' "$body" | grep -oE "$markers" | wc -l | tr -d ' ')

  if [ "$raw_hits" -eq 0 ]; then
    printf '\n  %s——%s    %-42s %sno credential-shaped values to strip%s\n' \
      "$dim" "$off" "your Caddy config" "$dim" "$off"
  else
    check "credentials in your config stay on this host" "0" "$sent_hits" \
      "$raw_hits credential-shaped value(s) are in the running config and $sent_hits reached the wire."
  fi

  raw_bytes=$(printf '%s' "$raw" | wc -c | tr -d ' ')
  sent_bytes=$(printf '%s' "$body" | wc -c | tr -d ' ')
  printf '  %s——%s    %-42s %s%s bytes -> %s bytes%s\n' \
    "$dim" "$off" "size of config vs what is sent" "$dim" "$raw_bytes" "$sent_bytes" "$off"
fi

# --- verdict ------------------------------------------------------------------

printf '\n'
if [ "$fail" -eq 0 ]; then
  printf '  %s%sAll %d checks passed.%s It is doing what it says.\n\n' "$bold" "$green" "$pass" "$off"
  exit 0
fi
printf '  %s%s%d of %d checks failed.%s Do not connect it until they pass.\n\n' \
  "$bold" "$red" "$fail" "$((pass + fail))" "$off"
exit 1
