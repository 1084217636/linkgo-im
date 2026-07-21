APP_NAME := linkgo-im

.PHONY: test fmt-check build docker-build compose-config compose-cn-config compose-light-config compose-light-cn-config observability-config observability-cn-config prometheus-check observability-up observability-cn-up observability-down observability-cn-down docker-up docker-cn-up docker-cn-reset docker-light-up docker-light-cn-up docker-down docker-cn-down docker-light-down docker-light-cn-down k8s-render k8s-check k8s-dry-run k8s-apply k8s-release k8s-delete ci-local bench ops-smoke fault-injection frontend-static-check core-im-demo frontend-smoke group-transfer-demo ai-config-check ai-test-suggest ai-quality-summary ai-demo ai-ask-demo im-ai-final-demo

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

prometheus-check:
	docker run --rm -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" $${PROMETHEUS_IMAGE:-prom/prometheus:v2.55.1} promtool check config /etc/prometheus/prometheus.yml

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

k8s-check:
	bash scripts/validate_k8s.sh

k8s-dry-run:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl apply --dry-run=client -f -

k8s-apply:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl apply -f -

k8s-release:
	test -n "$(IMAGE)"
	bash scripts/k8s_release.sh "$(IMAGE)"

k8s-delete:
	kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone | kubectl delete -f -

ci-local: fmt-check test build compose-config compose-cn-config compose-light-config compose-light-cn-config observability-config observability-cn-config docker-build

bench:
	bash benchmark/run_bench.sh

ops-smoke:
	bash scripts/ops_smoke.sh

fault-injection:
	FAULT_INJECTION_CONFIRM=1 bash scripts/fault_injection.sh

core-im-demo:
	bash scripts/demo_core_im.sh

frontend-smoke:
	bash scripts/frontend_smoke.sh

frontend-static-check:
	python3 scripts/validate_frontend.py

group-transfer-demo:
	bash scripts/demo_group_transfer.sh

ai-config-check:
	python3 tools/ai_agent_workflow/config_check.py --config-dir examples/game_config --output artifacts/config_check_report.json

ai-test-suggest:
	python3 tools/ai_agent_workflow/test_suggest.py --root . --output artifacts/test_suggestions.json

ai-quality-summary:
	python3 tools/ai_agent_workflow/quality_summary.py --task-type local_validation --validation-command "GOCACHE=/tmp/go-build go test ./..." --output artifacts/quality_summary.json

ai-demo:
	bash scripts/ai_demo.sh

ai-ask-demo:
	bash scripts/ai_ask_demo.sh

im-ai-final-demo:
	bash scripts/final_im_ai_demo.sh
