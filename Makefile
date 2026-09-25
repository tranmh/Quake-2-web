# Top-level entry points. See docs/plans/0001-port-plan.md.
.PHONY: demo oracle fixtures gen gen-check test test-go test-ts golden parity dev e2e lint

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

lint:
	cd server && go vet ./... && gofmt -l . | (! grep .)
