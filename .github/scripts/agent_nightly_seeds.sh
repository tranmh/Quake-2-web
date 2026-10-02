#!/usr/bin/env bash
# Plays one agent-nightly matrix entry: `q2bot run` once per seed, up to
# $PARALLEL seeds at a time (lockstep runs are deterministic under load),
# and checks each run's exit code against the entry's expectation.
#
# Usage: agent_nightly_seeds.sh <q2bot binary> <out dir>
#
# Environment (from the matrix entry):
#   NAME          the entry's name (agent-<NAME> artifact)
#   ARGS          q2bot run flags (word-split on purpose)
#   EXPECT        pass = exit 0; fail = exit 1; measure = 0 or 1. Any other
#                 code (2: a usage error) fails the entry.
#   SEEDS         the seeds to play, space-separated (default "1")
#   MIN_WINS      the report's cross-seed gate: at least this many of the
#                 seeds reach victory.pcx (default 0: no gate)
#   GATE          "true" when every run must pass the provenance gate
#   Q2BOT_COMMON  the flags every run shares (no -seed)
#   PARALLEL      runs at a time (default min(nproc, 3))
#
# Writes <out>/matrix.json (what the report checks) and, per seed,
# <out>/seed-<n>/{q2bot-run.txt, exit-code, <run id>/...}.
set -uo pipefail

q2bot=${1:?usage: agent_nightly_seeds.sh <q2bot> <out dir>}
out=${2:?usage: agent_nightly_seeds.sh <q2bot> <out dir>}
: "${NAME:?}" "${ARGS:?}" "${EXPECT:?}"
seeds=${SEEDS:-1}
min_wins=${MIN_WINS:-0}
parallel=${PARALLEL:-$(nproc)}
((parallel > 3)) && parallel=3
((parallel < 1)) && parallel=1

[[ "$seeds" =~ ^[0-9]+( [0-9]+)*$ ]] || { echo "::error::$NAME: seeds '$seeds' are not a list of numbers"; exit 1; }
[[ "$min_wins" =~ ^[0-9]+$ ]] || { echo "::error::$NAME: min_wins '$min_wins' is not a number"; exit 1; }
case "$EXPECT" in pass | fail | measure) ;; *) echo "::error::$NAME: expect '$EXPECT'"; exit 1 ;; esac
mkdir -p "$out"
NAME="$NAME" EXPECT="$EXPECT" SEEDS="$seeds" MIN_WINS="$min_wins" GATE="${GATE:-false}" ARGS="$ARGS" python3 -c '
import json, os, sys
e = os.environ
json.dump({"name": e["NAME"], "expect": e["EXPECT"], "seeds": [int(s) for s in e["SEEDS"].split()],
           "min_wins": int(e["MIN_WINS"]), "gate": e["GATE"] == "true", "args": e["ARGS"]}, sys.stdout, indent=1)
' > "$out/matrix.json" || exit 1

play() {
	local s=$1 d="$out/seed-$1"
	mkdir -p "$d"
	# $ARGS and $Q2BOT_COMMON are flag lists: word splitting is intended
	# shellcheck disable=SC2086
	"$q2bot" run $ARGS ${Q2BOT_COMMON:-} -seed "$s" -out "$d" >"$d/q2bot-run.txt" 2>&1
	echo "$?" >"$d/exit-code"
}

for s in $seeds; do
	while (($(jobs -rp | wc -l) >= parallel)); do wait -n; done
	play "$s" &
done
wait

bad=0
for s in $seeds; do
	d="$out/seed-$s"
	code=$(cat "$d/exit-code" 2>/dev/null || echo missing)
	echo "::group::$NAME seed $s: exit $code"
	cat "$d/q2bot-run.txt" 2>/dev/null
	echo "::endgroup::"
	case "$EXPECT:$code" in
	pass:0 | fail:1 | measure:0 | measure:1) echo "q2bot run $NAME seed $s exited $code (expected: $EXPECT)" ;;
	*)
		echo "::error::q2bot run $NAME seed $s exited $code, expected $EXPECT"
		bad=1
		;;
	esac
done
exit "$bad"
