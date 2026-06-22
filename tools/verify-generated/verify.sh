#!/usr/bin/env bash
#
# verify.sh - Ensure the committed vendored PCRE2 sources match what
# generate.sh produces (i.e. there is no manual, unrecorded drift).
#
# Re-runs the generator into a temporary directory and diffs the upstream
# files. Hand-written files (wrapper.c, wrapper.h, *.go) are not generated and
# therefore not compared.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMMITTED="${MODULE_ROOT}/internal/pcre2lite"
TMP_OUT="$(mktemp -d)"
trap 'rm -rf "${TMP_OUT}"' EXIT

"${MODULE_ROOT}/tools/generate-pcre2lite/generate.sh" "${TMP_OUT}" >&2

status=0
while IFS= read -r -d '' f; do
  name="$(basename "${f}")"
  if [ ! -f "${COMMITTED}/${name}" ]; then
    echo "MISSING in repo: ${name}"
    status=1
    continue
  fi
  if ! diff -q "${f}" "${COMMITTED}/${name}" >/dev/null; then
    echo "DRIFT: ${name}"
    status=1
  fi
done < <(find "${TMP_OUT}" -maxdepth 1 -type f -print0)

if [ "${status}" -eq 0 ]; then
  echo "OK: vendored PCRE2 sources match generate.sh output"
else
  echo "FAIL: vendored sources drifted from generate.sh output" >&2
fi
exit "${status}"
