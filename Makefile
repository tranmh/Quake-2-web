# Top-level entry points. See docs/plans/0001-port-plan.md.
.PHONY: demo oracle fixtures gen gen-check test test-go test-ts golden parity dev e2e lint nav nav-verify agent-smoke

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
