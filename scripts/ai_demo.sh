#!/usr/bin/env bash
set -euo pipefail

python3 tools/ai_agent_workflow/config_check.py \
  --config-dir examples/game_config \
  --output artifacts/config_check_report.json

python3 tools/ai_agent_workflow/test_suggest.py \
  --root . \
  --output artifacts/test_suggestions.json

python3 tools/ai_agent_workflow/quality_summary.py \
  --task-type local_validation \
  --validation-command "GOCACHE=/tmp/go-build go test ./..." \
  --output artifacts/quality_summary.json
