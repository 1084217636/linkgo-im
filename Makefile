APP_NAME := linkgo-im

.PHONY: test build docker-build compose-config docker-up docker-down k8s-apply k8s-delete ci-local bench ai-config-check ai-test-suggest ai-quality-summary ai-demo

test:
	go test ./...

build:
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/logic ./cmd/logic
	go build -o bin/transfer ./cmd/transfer

docker-build:
	docker build -t $(APP_NAME):local .

compose-config:
	docker compose config

docker-up:
	docker compose up --build

docker-down:
	docker compose down

k8s-apply:
	kubectl apply -f deploy/k8s/

k8s-delete:
	kubectl delete -f deploy/k8s/

ci-local: test build compose-config docker-build

bench:
	bash benchmark/run_bench.sh

ai-config-check:
	python3 tools/ai_agent_workflow/config_check.py --config-dir examples/game_config --output artifacts/config_check_report.json

ai-test-suggest:
	python3 tools/ai_agent_workflow/test_suggest.py --root . --output artifacts/test_suggestions.json

ai-quality-summary:
	python3 tools/ai_agent_workflow/quality_summary.py --task-type local_validation --validation-command "GOCACHE=/tmp/go-build go test ./..." --output artifacts/quality_summary.json

ai-demo: ai-config-check ai-test-suggest ai-quality-summary
