#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
bash -n bootstrap.sh
if [[ -n "$(gofmt -l clients/lib/go/*.go clients/cli/*.go)" ]]; then
  printf 'Go files need gofmt\n' >&2
  exit 1
fi
(cd clients/lib/go && go test -race ./... && go vet ./...)
(cd clients/cli && go test -race ./... && go vet ./...)
npm test
npm --prefix clients/agents/plugins/pi test
