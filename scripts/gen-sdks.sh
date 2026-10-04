#!/usr/bin/env bash
# gen-sdks.sh — generate long-tail-language clients from openapi.json.
#
# Hand-written SDKs live in sdk/{python,javascript} and client/ (Go) — those
# are the supported surfaces. This script exists for languages we don't
# hand-maintain (rust, ruby, java, php, csharp…): any target openapi-generator
# supports works here.
#
# Usage:
#   scripts/gen-sdks.sh rust ruby java        # generate into sdk/gen/<lang>/
#   scripts/gen-sdks.sh --list                # list available generators
#   WEBX_GEN_OUT=/tmp/clients scripts/gen-sdks.sh rust
#
# Uses the dockerized generator when docker is up (no Java needed), else
# falls back to npx @openapitools/openapi-generator-cli (needs java + npx).
set -euo pipefail
cd "$(dirname "$0")/.."

SPEC=internal/serve/openapi.json
OUT=${WEBX_GEN_OUT:-sdk/gen}
LANGS=${*:-rust ruby java}

run_gen() { # $1=lang $2=dest-on-host
	local lang=$1 dest=$2
	if docker info > /dev/null 2>&1; then
		mkdir -p "$dest"
		docker run --rm -v "$PWD:/work" -v "$(cd "$dest" && pwd):/out" -w /work \
			openapitools/openapi-generator-cli:latest \
			generate -i "$SPEC" -g "$lang" -o "/out" \
			--package-name webx > /dev/null
	else
		npx -y @openapitools/openapi-generator-cli@latest \
			generate -i "$SPEC" -g "$lang" -o "$dest" \
			--package-name webx > /dev/null
	fi
	test -n "$(ls -A "$dest")" || { echo "!! $lang produced no output"; exit 1; }
}

if [[ "${1:-}" == "--list" ]]; then
	if docker info > /dev/null 2>&1; then
		exec docker run --rm openapitools/openapi-generator-cli:latest list
	fi
	exec npx -y @openapitools/openapi-generator-cli@latest list
fi

for lang in $LANGS; do
	dest="$OUT/$lang"
	echo "== generating $lang -> $dest"
	run_gen "$lang" "$dest"
done
echo "done — $OUT/{${LANGS// /,}}"
