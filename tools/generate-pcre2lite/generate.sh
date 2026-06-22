#!/usr/bin/env bash
#
# generate.sh - Reproducibly vendor a trimmed PCRE2 8-bit interpreter into the
# go-pcre2-lite module.
#
# This script downloads a FIXED upstream PCRE2 release, verifies its checksum,
# selects only the source files required for the 8-bit interpreter, injects the
# build configuration (8-bit only, Unicode on, JIT permanently disabled), and
# writes the result into the target directory (default: internal/pcre2lite).
#
# JIT is permanently disabled: no SLJIT sources, no pcre2_jit_*.c, and
# SUPPORT_JIT is never defined.
#
# Usage:
#   tools/generate-pcre2lite/generate.sh [OUTPUT_DIR]
#
# The build configuration is fully driven by the constants below so that the
# output is byte-for-byte reproducible. tools/verify-generated/verify.sh runs
# this script into a temporary directory and diffs against the committed files.

set -euo pipefail

PCRE2_VERSION="10.42"
PCRE2_TARBALL="pcre2-${PCRE2_VERSION}.tar.gz"
PCRE2_URL="https://github.com/PCRE2Project/pcre2/releases/download/pcre2-${PCRE2_VERSION}/${PCRE2_TARBALL}"
PCRE2_SHA256="c33b418e3b936ee3153de2c61cc638e7e4fe3156022a5c77d0711bcbb9d64f1f"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUTPUT_DIR="${1:-${MODULE_ROOT}/internal/pcre2lite}"
CACHE_DIR="${PCRE2LITE_CACHE_DIR:-${TMPDIR:-/tmp}/pcre2lite-cache}"

# C source files required for the 8-bit interpreter (each compiled standalone).
# JIT, DFA, convert, serialize, POSIX and the command line tools are excluded.
SOURCES=(
  pcre2_auto_possess.c
  pcre2_compile.c
  pcre2_config.c
  pcre2_context.c
  pcre2_error.c
  pcre2_extuni.c
  pcre2_find_bracket.c
  pcre2_maketables.c
  pcre2_match.c
  pcre2_match_data.c
  pcre2_newline.c
  pcre2_ord2utf.c
  pcre2_pattern_info.c
  pcre2_script_run.c
  pcre2_string_utils.c
  pcre2_study.c
  pcre2_substring.c
  pcre2_tables.c
  pcre2_ucd.c
  pcre2_valid_utf.c
  pcre2_xclass.c
)

# Internal headers required by the sources above.
HEADERS=(
  pcre2_internal.h
  pcre2_intmodedep.h
  pcre2_ucp.h
)

# Data files that are #included by a source file rather than compiled on their
# own. They are renamed to a ".inc" suffix so that the Go toolchain does not try
# to compile them as standalone translation units.
INCLUDED=(
  pcre2_ucptables.c
)

log() { printf '[generate-pcre2lite] %s\n' "$*" >&2; }

require() {
  command -v "$1" >/dev/null 2>&1 || { log "ERROR: required tool not found: $1"; exit 1; }
}

require curl
require tar
require sed
if command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD="sha256sum"
else
  log "ERROR: need shasum or sha256sum"; exit 1
fi

mkdir -p "${CACHE_DIR}"
TARBALL_PATH="${CACHE_DIR}/${PCRE2_TARBALL}"

if [ ! -f "${TARBALL_PATH}" ]; then
  log "downloading ${PCRE2_URL}"
  curl -sSL --max-time 300 -o "${TARBALL_PATH}" "${PCRE2_URL}"
fi

ACTUAL_SHA="$(${SHA_CMD} "${TARBALL_PATH}" | awk '{print $1}')"
if [ "${ACTUAL_SHA}" != "${PCRE2_SHA256}" ]; then
  log "ERROR: checksum mismatch for ${TARBALL_PATH}"
  log "  expected ${PCRE2_SHA256}"
  log "  actual   ${ACTUAL_SHA}"
  exit 1
fi
log "checksum verified: ${PCRE2_SHA256}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT
tar xzf "${TARBALL_PATH}" -C "${WORK_DIR}"
SRC="${WORK_DIR}/pcre2-${PCRE2_VERSION}/src"
ROOT="${WORK_DIR}/pcre2-${PCRE2_VERSION}"

mkdir -p "${OUTPUT_DIR}"

# Remove previously generated upstream files so deletions upstream propagate.
rm -f "${OUTPUT_DIR}"/pcre2_*.c "${OUTPUT_DIR}"/pcre2_*.c.inc \
      "${OUTPUT_DIR}/pcre2.h" "${OUTPUT_DIR}/config.h" \
      "${OUTPUT_DIR}/pcre2_config.h" "${OUTPUT_DIR}/pcre2_internal.h" \
      "${OUTPUT_DIR}/pcre2_intmodedep.h" "${OUTPUT_DIR}/pcre2_ucp.h" \
      "${OUTPUT_DIR}/upstream-version.txt" 2>/dev/null || true

log "copying ${#SOURCES[@]} source files"
for f in "${SOURCES[@]}"; do
  cp "${SRC}/${f}" "${OUTPUT_DIR}/${f}"
done

log "copying ${#HEADERS[@]} headers"
for f in "${HEADERS[@]}"; do
  cp "${SRC}/${f}" "${OUTPUT_DIR}/${f}"
done

# Public header: the dist ships a pre-substituted pcre2.h.generic.
cp "${SRC}/pcre2.h.generic" "${OUTPUT_DIR}/pcre2.h"

# Default character tables: shipped as a ".dist" template.
cp "${SRC}/pcre2_chartables.c.dist" "${OUTPUT_DIR}/pcre2_chartables.c"

# Included-only data files: rename to .inc and patch their includer.
for f in "${INCLUDED[@]}"; do
  cp "${SRC}/${f}" "${OUTPUT_DIR}/${f}.inc"
done
# pcre2_tables.c pulls in pcre2_ucptables.c; point it at the .inc copy.
sed -i.bak 's|#include "pcre2_ucptables.c"|#include "pcre2_ucptables.c.inc"|' \
  "${OUTPUT_DIR}/pcre2_tables.c"
rm -f "${OUTPUT_DIR}/pcre2_tables.c.bak"

# Build configuration. Start from the upstream generic config and enable only
# what the 8-bit interpreter needs. SUPPORT_JIT is intentionally left undefined.
log "generating pcre2_config.h"
cp "${SRC}/config.h.generic" "${OUTPUT_DIR}/pcre2_config.h"
config_enable() {
  # turn "/* #undef NAME */" into "#define NAME VALUE"
  local name="$1" value="$2"
  sed -i.bak "s|/\\* #undef ${name} \\*/|#define ${name} ${value}|" \
    "${OUTPUT_DIR}/pcre2_config.h"
  rm -f "${OUTPUT_DIR}/pcre2_config.h.bak"
}
config_enable SUPPORT_PCRE2_8 1
config_enable SUPPORT_UNICODE 1
config_enable PCRE2_STATIC 1
config_enable HAVE_MEMMOVE 1

# Guard against accidental JIT re-enablement in the generated config.
if grep -q '^#define SUPPORT_JIT' "${OUTPUT_DIR}/pcre2_config.h"; then
  log "ERROR: SUPPORT_JIT must never be defined"
  exit 1
fi

# config.h shim so that the upstream "#include \"config.h\"" resolves to ours.
cat > "${OUTPUT_DIR}/config.h" <<'EOF'
/* go-pcre2-lite: generated shim. Upstream PCRE2 sources include "config.h";
   the real configuration lives in pcre2_config.h. Do not edit by hand. */
#ifndef PCRE2LITE_CONFIG_SHIM_H
#define PCRE2LITE_CONFIG_SHIM_H
#include "pcre2_config.h"
#endif
EOF

# Third-party license + upstream provenance.
mkdir -p "${MODULE_ROOT}/THIRD_PARTY_LICENSES"
if [ -f "${ROOT}/LICENCE" ]; then
  cp "${ROOT}/LICENCE" "${MODULE_ROOT}/THIRD_PARTY_LICENSES/PCRE2-LICENSE"
fi

GEN_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
{
  echo "PCRE2 upstream vendored by tools/generate-pcre2lite/generate.sh"
  echo "version: ${PCRE2_VERSION}"
  echo "tarball: ${PCRE2_TARBALL}"
  echo "url: ${PCRE2_URL}"
  echo "sha256: ${PCRE2_SHA256}"
  echo "generated: ${GEN_DATE}"
  echo "jit: permanently-disabled (SUPPORT_JIT never defined)"
  echo "widths: 8-bit only"
  echo
  echo "sources:"
  for f in "${SOURCES[@]}"; do echo "  ${f}"; done
  echo "  pcre2_chartables.c (from pcre2_chartables.c.dist)"
  echo "headers:"
  for f in "${HEADERS[@]}"; do echo "  ${f}"; done
  echo "  pcre2.h (from pcre2.h.generic)"
  echo "  pcre2_config.h (from config.h.generic, patched)"
  echo "included (not compiled standalone):"
  for f in "${INCLUDED[@]}"; do echo "  ${f}.inc (from ${f})"; done
} > "${OUTPUT_DIR}/upstream-version.txt"

log "done -> ${OUTPUT_DIR}"
