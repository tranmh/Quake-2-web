#!/usr/bin/env bash
# Starts q2server (bots on) and the Next.js dev server together, then opens the browser:
#   tools/bots-web.sh watch    a public server-owned bot plays $BOT_MAPS live: /watch/<id>
#   tools/bots-web.sh replay   a finished run of $RUNS: /bots/<id> ($RUN, default the newest)
#   tools/bots-web.sh web      the bots page only: /bots
# Reads .env (TYPESAFE_API_KEY for BACKEND=jev, ...), then forces the local-dev settings below:
# bots on, plain-http cookies, the in-memory database (USE_DB=1: .env's DATABASE_URL), OPEN=0: no
# browser. Ctrl-C stops both servers. Used by `make watch`, `make replay` and `make web`.
set -euo pipefail

mode=${1:-web}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

RUNS=${RUNS:-runs}
BOT_MAPS=${BOT_MAPS:-demo1,demo2,demo3}
BACKEND=${BACKEND:-scripted}
WEB_PORT=${WEB_PORT:-3000}
web="http://localhost:$WEB_PORT"

mkdir -p "$RUNS"
# the in-memory repository unless USE_DB=1 (then .env's DATABASE_URL: accounts and the daily Jev
# spend persist)
[[ "${USE_DB:-0}" == 1 ]] || export DATABASE_URL=
export Q2_BOTS_ENABLED=true Q2_BOTS_ALLOW_USERS=true Q2_COOKIE_SECURE=false Q2_LOG_FORMAT=text
export Q2_CORS_ORIGINS="$web" Q2_BOT_RUNS_DIR="$(cd "$RUNS" && pwd)"
export Q2_DEMO_PAK=${Q2_DEMO_PAK:-assets/demo/baseq2/pak0.pak}
export Q2_HTTP_ADDR=${Q2_HTTP_ADDR:-:8080}
api="http://localhost:${Q2_HTTP_ADDR##*:}"
export Q2_SERVER_URL="$api"

[[ -f "$Q2_DEMO_PAK" ]] || { echo "no demo pak at $Q2_DEMO_PAK: run make demo" >&2; exit 1; }
if [[ "$BACKEND" == jev && -z "${TYPESAFE_API_KEY:-}" ]]; then
  echo "BACKEND=jev needs TYPESAFE_API_KEY in .env" >&2
  exit 1
fi
for port in "${Q2_HTTP_ADDR##*:}" "$WEB_PORT"; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "port $port is already in use (another server running?)" >&2
    exit 1
  fi
done

replay_id=
if [[ "$mode" == replay ]]; then
  replay_id=${RUN:-}
  if [[ -z "$replay_id" ]]; then
    replay_id=$(ls -1t "$RUNS" 2>/dev/null | while read -r d; do [[ -f "$RUNS/$d/run.json" ]] && { echo "$d"; break; }; done || true)
  fi
  replay_id=$(basename "$replay_id")
  [[ -n "$replay_id" && -f "$RUNS/$replay_id/run.json" ]] ||
    { echo "no finished run in $RUNS (make bot-run, or RUNS=<dir> / RUN=<id>)" >&2; exit 1; }
fi

# `make dev` (docker) leaves root-owned files in web/apps/web/.next/dev, and next dev then cannot take
# its lock: move them aside (.next is ours) and start a fresh dev cache
dev=web/apps/web/.next/dev
if [[ -d "$dev" ]] && [[ -n "$(find "$dev" ! -user "$(id -u)" -print -quit 2>/dev/null)" ]]; then
  aside="$dev.root-owned.$(date +%s)"
  mv "$dev" "$aside"
  echo "moved the root-owned Next.js dev cache to $aside (remove it: sudo rm -rf $aside)"
fi

echo "building q2server..."
(cd server && go build -o bin/q2server ./cmd/q2server)

pids=()
cleanup() {
  trap - EXIT INT TERM
  # each server runs in its own process group: pnpm's next dev child goes too
  for p in "${pids[@]}"; do kill -TERM -- "-$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

botargs=()
[[ "$mode" == watch ]] && botargs=(-bot "$BOT_MAPS" -bot-backend "$BACKEND")
setsid server/bin/q2server "${botargs[@]}" &
pids+=($!)
(cd web && exec setsid pnpm --filter web dev) &
pids+=($!)

wait_up() { # url, what
  for _ in $(seq 1 240); do
    curl -fsS -o /dev/null "$1" 2>/dev/null && return 0
    for p in "${pids[@]}"; do kill -0 "$p" 2>/dev/null || { echo "$2 exited" >&2; exit 1; }; done
    sleep 0.5
  done
  echo "$2 did not come up at $1" >&2
  exit 1
}
wait_up "$api/healthz" q2server
wait_up "$web/bots" "the web dev server"

page="$web/bots"
case "$mode" in
replay) page="$web/bots/$replay_id" ;;
watch)
  id=
  for _ in $(seq 1 60); do
    id=$(curl -fsS "$api/api/v1/bots" 2>/dev/null | python3 -c '
import json, sys
bots = json.load(sys.stdin).get("bots") or []
live = [b for b in bots if b.get("status") in ("starting", "running")]
print(live[0]["id"] if live else "")' 2>/dev/null || true)
    [[ -n "$id" ]] && break
    sleep 0.5
  done
  [[ -n "$id" ]] && page="$web/watch/$id"
  ;;
esac

echo
echo "  open: $page"
echo "  runs: $Q2_BOT_RUNS_DIR (finished runs replay at $web/bots)"
echo "  Ctrl-C stops both servers"
echo
command -v xdg-open >/dev/null && [[ "${OPEN:-1}" == 1 ]] && xdg-open "$page" >/dev/null 2>&1 || true
wait -n "${pids[@]}"
