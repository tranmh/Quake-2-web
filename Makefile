# Top-level entry points. See docs/plans/0001-port-plan.md. `make help` lists the AI bot shortcuts.
.PHONY: demo oracle fixtures gen gen-check test test-go test-ts golden parity dev e2e lint nav nav-verify agent-smoke \
  help q2bot watch replay web bot-run jev-probe jev-run validate replay-check runs

demo:
	tools/fetch-demo-pak.sh

oracle:
	oracle/sync.sh
	$(MAKE) -C oracle -j$$(nproc)

fixtures: oracle
	tools/gen-fixtures.sh

gen:
	cd server && go run ./cmd/genconst -oracle ../Quake-2 \
	  -go internal/q2const/const_gen.go \
	  -ts ../web/packages/q2-shared/src/generated/const.ts

gen-check: gen
	git diff --exit-code -- server/internal/q2const web/packages/q2-shared/src/generated

test: test-go test-ts

test-go:
	cd server && go vet ./... && go test ./...

test-ts:
	cd web && pnpm -r test

golden:
	cd server && Q2_FIXTURES=$(CURDIR)/fixtures/generated go test -tags golden ./...
	cd web && Q2_FIXTURES=$(CURDIR)/fixtures/generated pnpm -r test

parity:
	tools/parity-check.sh
	tools/check-oracle-untouched.sh

dev:
	docker compose -f deploy/docker-compose.dev.yml up --build

# Playwright against a real q2server + next start (web/apps/web/e2e/global-setup.ts builds both).
# Q2_E2E_REQUIRE=1 turns the self-skips (no Chromium, no Go, no demo pak, bots off) into failures.
e2e:
	cd web && pnpm --filter web e2e

# The agent's nav graphs for the demo maps (gzipped JSON, cache key = map checksum + format + physics
# hash; up-to-date graphs are kept) plus the dev overlay's dumps (web/apps/dev ?nav=1), all in
# assets/nav, which is not committed. Without it the agent builds missing graphs on first use.
NAV_PAK ?= $(CURDIR)/assets/demo/baseq2/pak0.pak
NAV_DIR ?= $(CURDIR)/assets/nav
NAV_MAPS ?= demo1 demo2 demo3

nav: demo
	cd server && for m in $(NAV_MAPS); do go run ./cmd/q2nav build -pak $(NAV_PAK) -map $$m -out $(NAV_DIR) || exit 1; done
	cd server && for m in $(NAV_MAPS); do go run ./cmd/q2nav dump -pak $(NAV_PAK) -map $$m -nav $(NAV_DIR) -o $(NAV_DIR)/$$m.viz.json || exit 1; done

# Re-simulates sampled edges, runs a sample on a live lockstep server and checks the route tables.
nav-verify: nav
	cd server && for m in $(NAV_MAPS); do go run ./cmd/q2nav verify -pak $(NAV_PAK) -map $$m -nav $(NAV_DIR) -sample 300 -live 100 -posed 50 || exit 1; done

# Two lockstep runs of demo1 into a temporary directory, each checked by `q2bot validate` (traces,
# demos): the scripted bot, then the noisy mock Jev (the real jev client against an in-process fake
# server, 212 ms simulated latency), which must finish the level and pass the provenance gate at 0.7
# (target, fire_policy and mode each decided by the model). Deterministic; about 10 s of wall time
# after the build.
# The directory is removed on success and kept (and printed) on failure.
agent-smoke: demo
	@out=$$(mktemp -d); echo "agent-smoke: $$out"; \
	if (cd server && go build -o "$$out/q2bot" ./cmd/q2bot) && \
	   "$$out/q2bot" run -backend scripted -session lockstep -maps demo1 -require-complete -out "$$out/runs" && \
	   "$$out/q2bot" validate "$$(ls -d "$$out"/runs/*/ | head -1)" && \
	   "$$out/q2bot" run -backend mock -mock-policy noisy -session lockstep -maps demo1 -require-complete \
	     -min-model-share 0.7 -out "$$out/mock" && \
	   "$$out/q2bot" validate "$$(ls -d "$$out"/mock/*/ | head -1)" -min-model-share 0.7; \
	then rm -rf "$$out"; else echo "agent-smoke failed: see $$out" >&2; exit 1; fi

lint:
	cd server && go vet ./... && gofmt -l . | (! grep .)

# ---- AI bot shortcuts. They read .env (TYPESAFE_API_KEY for BACKEND=jev); see docs/AGENT.md. ----
#   make watch [BACKEND=jev] [BOT_MAPS=demo1]   watch a bot play live in the browser
#   make bot-run [BACKEND=mock] [MAPS=demo1]    a headless run into $(RUNS), then make replay
#   make replay [RUN=<id>]                      replay a finished run in the browser (default the newest)
#   make jev-probe / make jev-run [BUDGET=1]    live Jev API checks (costs real money, capped)
RUNS ?= runs
BACKEND ?= scripted
MAPS ?=
BOT_MAPS ?= demo1,demo2,demo3
BUDGET ?= 1
RUN ?=
export RUNS BACKEND BOT_MAPS RUN

Q2BOT := server/bin/q2bot
LOADENV := set -a; [ ! -f .env ] || . ./.env; set +a;
# the evaluation settings of docs/AGENT-EVAL.md (the death cap lets demo3 be retried)
BOT_FLAGS := -session lockstep -skill 1 -max-deaths 25 -level-timeout 60m $(if $(MAPS),-maps $(MAPS))
# RUN, else the newest run of $(RUNS) with a run.json
RUN_DIR = d="$(RUN)"; [ -n "$$d" ] || d=$$(for x in $$(ls -1t $(RUNS) 2>/dev/null); do [ -f "$(RUNS)/$$x/run.json" ] && { echo "$$x"; break; }; done); \
  d=$(RUNS)/$$(basename "$$d"); [ -f "$$d/run.json" ] || { echo "no finished run in $(RUNS) (make bot-run, or RUN=<id>)" >&2; exit 1; };

help:
	@echo "AI bot:"
	@echo "  make watch [BACKEND=scripted|mock|jev] [BOT_MAPS=demo1,demo2,demo3]  live bot in the browser"
	@echo "  make replay [RUN=<id>] [RUNS=runs]     replay a finished run in the browser (default: newest)"
	@echo "  make web                                server + web app, bots page only"
	@echo "  make bot-run [BACKEND=...] [MAPS=demo1] headless lockstep run into RUNS"
	@echo "  make runs                               list runs with outcome and cost"
	@echo "  make validate [RUN=<id>]                check traces, demos and run.json"
	@echo "  make replay-check [RUN=<id>]            re-run the recorded episodes, no API calls"
	@echo "  make jev-probe                          a few live Jev requests (well under a cent)"
	@echo "  make jev-run [BUDGET=1] [MAPS=...]      budget-capped live Jev campaign + validate"
	@echo "  make agent-smoke                        scripted + mock smoke runs"
	@echo "Setup: make demo nav. Web: http://localhost:3000. Ctrl-C stops watch/replay/web."

$(Q2BOT): FORCE
	cd server && go build -o bin/q2bot ./cmd/q2bot
q2bot: $(Q2BOT)
FORCE:

watch: demo
	tools/bots-web.sh watch

replay:
	tools/bots-web.sh replay

web: demo
	tools/bots-web.sh web

bot-run: demo $(Q2BOT)
	@$(LOADENV) $(Q2BOT) run -backend $(BACKEND) $(BOT_FLAGS) $(if $(filter jev,$(BACKEND)),-budget-usd $(BUDGET) -on-exhausted stop) -out $(RUNS)

runs:
	@for d in $$(ls -1t $(RUNS) 2>/dev/null); do [ -f "$(RUNS)/$$d/run.json" ] || continue; \
	  python3 -c 'import json, sys; r = json.load(open(sys.argv[1])); t = r["totals"]; \
	print("%-26s %-8s %-10s %2d deaths  $$%.4f  %s" % (sys.argv[2], r["backend"], r["outcome"], t["deaths"], r["api"]["cost_usd"], r.get("reason", "")))' \
	  "$(RUNS)/$$d/run.json" "$$d" 2>/dev/null || echo "$$d"; done

validate: $(Q2BOT)
	@$(RUN_DIR) $(Q2BOT) validate "$$d"

replay-check: $(Q2BOT)
	@$(RUN_DIR) for t in "$$d"/ep-*/trace.jsonl.gz; do $(Q2BOT) replay -trace "$$t" -mode responses || exit 1; done

jev-probe: $(Q2BOT)
	@$(LOADENV) mkdir -p $(RUNS) && $(Q2BOT) jev-probe -lane fast -n 3 -out $(RUNS)/live-probe-fast.json && \
	  $(Q2BOT) jev-probe -lane slow -n 1 -out $(RUNS)/live-probe-slow.json

# the manual jev-live CI job's campaign (.github/workflows/agent-nightly.yml)
jev-run: demo nav $(Q2BOT)
	@$(LOADENV) $(Q2BOT) run -backend jev -sim-latency 212ms -budget-usd $(BUDGET) -on-exhausted stop \
	  -require-complete -min-model-share 0.7 $(BOT_FLAGS) -out $(RUNS)
	@$(MAKE) --no-print-directory validate RUN=
