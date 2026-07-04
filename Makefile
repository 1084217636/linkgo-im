APP_NAME := linkgo-im

.PHONY: test fmt-check build docker-build compose-config compose-cn-config compose-light-config compose-light-cn-config observability-config observability-cn-config observability-up observability-cn-up observability-down observability-cn-down docker-up docker-cn-up docker-cn-reset docker-light-up docker-light-cn-up docker-down docker-cn-down docker-light-down docker-light-cn-down k8s-render k8s-dry-run k8s-apply k8s-delete ci-local bench ops-smoke core-im-demo ai-config-check ai-test-suggest ai-quality-summary ai-demo

test:
	go test ./...

fmt-check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

build:
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/logic ./cmd/logic
	go build -o bin/transfer ./cmd/transfer

docker-build:
	docker build -t $(APP_NAME):local .

compose-config:
	docker compose config

compose-cn-config:
	docker compose --env-file .env.docker-cn config

compose-light-config:
	docker compose -f docker-compose.light.yml config

compose-light-cn-config:
	docker compose --env-file .env.docker-cn -f docker-compose.light.yml config

observability-config:
	docker compose -f docker-compose.yml -f docker-compose.observability.yml config

observability-cn-config:
	docker compose --env-file .env.docker-cn -f docker-compose.yml -f docker-compose.observability.yml config

observability-up:
	docker compose -f docker-compose.yml -f docker-compose.observability.yml up --build

observability-cn-up:
	docker compose --env-file .env.docker-cn -f docker-compose.yml -f docker-compose.observability.yml up --build

observability-down:
	docker compose -f docker-compose.yml -f docker-compose.observability.yml down

observability-cn-down:
	docker compose --env-file .env.docker-cn -f docker-compose.yml -f docker-compose.observability.yml down

docker-up:
	docker compose up --build

docker-cn-up:
	docker compose --env-file .env.docker-cn up --build

docker-cn-reset:
	docker compose --env-file .env.docker-cn down -v --remove-orphans
	docker compose --env-file .env.docker-cn up --build

docker-light-up:
	docker compose -f docker-compose.light.yml up --build

docker-light-cn-up:
	docker compose --env-file .env.docker-cn -f docker-compose.light.yml up --build

docker-down:
	docker compose down

docker-cn-down:
	docker compose --env-file .env.docker-cn down

docker-light-down:
	docker compose -f docker-compose.light.yml down

docker-light-cn-down:
	docker compose --env-file .env.docker-cn -f docker-compose.light.yml down

k8s-render:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone

k8s-dry-run:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl apply --dry-run=client -f -

k8s-apply:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl apply -f -

k8s-delete:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl delete -f -

ci-local: fmt-check test build compose-config compose-cn-config compose-light-config compose-light-cn-config observability-config observability-cn-config docker-build

bench:
	bash benchmark/run_bench.sh

ops-smoke:
	bash scripts/ops_smoke.sh

core-im-demo:
	bash scripts/demo_core_im.sh

ai-config-check:
	python3 tools/ai_agent_workflow/config_check.py --config-dir examples/game_config --output artifacts/config_check_report.json

ai-test-suggest:
	python3 tools/ai_agent_workflow/test_suggest.py --root . --output artifacts/test_suggestions.json

ai-quality-summary:
	python3 tools/ai_agent_workflow/quality_summary.py --task-type local_validation --validation-command "GOCACHE=/tmp/go-build go test ./..." --output artifacts/quality_summary.json

ai-demo: ai-config-check ai-test-suggest ai-quality-summary
