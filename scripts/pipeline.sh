#!/bin/sh
# Runs the delivery pipeline without a terminal session, so a GitHub issue can
# go from report to merged fix (and release) with one command:
#
#   scripts/pipeline.sh                          audit and resolve every open PR and issue
#   scripts/pipeline.sh 12                       only issue 12
#   scripts/pipeline.sh 12 release               issue 12, then cut a release
#   PIPELINE_AGENT=codex scripts/pipeline.sh 12  the same, driven by Codex
#
# PIPELINE_AGENT picks the agent: claude or codex. Unset or empty falls back
# to claude.
# PIPELINE_PERMISSION_MODE picks how Claude approves tool calls (default: auto).
# PIPELINE_CLAUDE_FLAGS and PIPELINE_CODEX_FLAGS append flags to the agent
# invocation.
#
# Either agent runs without approval prompts and with network access, because
# the pipeline pushes branches and calls gh. Run it on a machine or runner
# that is sandboxed from the outside.
set -eu
cd "$(dirname "$0")/.."

prompt="Use the pipeline skill from .agents/skills/pipeline/SKILL.md with these arguments: $*"

run_claude() {
  # shellcheck disable=SC2086
  exec claude -p "$prompt" \
    --permission-mode "${PIPELINE_PERMISSION_MODE:-auto}" \
    ${PIPELINE_CLAUDE_FLAGS:-}
}

run_codex() {
  # shellcheck disable=SC2086
  exec codex exec --dangerously-bypass-approvals-and-sandbox \
    ${PIPELINE_CODEX_FLAGS:-} \
    "$prompt"
}

case "${PIPELINE_AGENT:-claude}" in
  claude) run_claude ;;
  codex) run_codex ;;
  *)
    echo "PIPELINE_AGENT must be claude or codex, got '$PIPELINE_AGENT'" >&2
    exit 2
    ;;
esac
