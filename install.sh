#!/usr/bin/env sh
set -eu
cat <<'MESSAGE'
ShellBeam Monitoring Agent

This fork does not install or replace host services with a shell script.
Use the ShellBeam application's signed monitoring-probe management flow.
Creating a separate shellbeam-monitoring-agent.service is not supported by
existing legacy takeover detection.

For a manual foreground run, build/download the reviewed binary and use:
  shellbeam-monitoring-agent --config /absolute/path/to/agent.json

See docs/integration.md for the legacy service/path compatibility contract
and docs/releases.md for checksums, release identity, and signed promotion.
MESSAGE
