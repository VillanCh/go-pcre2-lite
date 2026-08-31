# Changelog

## v0.1.4 - 2026-08-31

- Remove `github.com/dlclark/regexp2` from the production module graph.
- Implement ECMAScript Annex B legacy decimal escapes, including the
  out-of-range `\2` form found in a 28 MiB production JavaScript bundle.
- Preserve literal hyphens next to ECMAScript set escapes such as `[^\s-_]`
  while expanding JavaScript whitespace semantics for PCRE2.
- Isolate the optional compatibility oracle and comparative benchmarks in the
  nested `differential` test module, so downstream users no longer inherit the
  reference engine or its checksums.
- Add CI enforcement that fails if the reference engine leaks back into the
  production module graph.

## v0.1.3 - 2026-08-31

- Add an ECMAScript compatibility rewrite for exact JavaScript `\s` / `\S`
  whitespace, dot line terminators, empty character classes, and JavaScript
  `\x` / `\u` / `\u{...}` escape semantics.
- Fix `FindRunesMatchStartingAt` panicking on ASCII input and add boundary
  coverage for ASCII, Unicode, end-of-input, and invalid start positions.
- Return the inspectable `ErrUnsupportedRune` from rune APIs for isolated
  UTF-16 surrogates and other values PCRE2 8-bit UTF cannot represent. This
  lets JavaScript runtimes safely route those rare inputs to a UTF-16 engine
  instead of silently replacing them with U+FFFD.
- Verify the compatibility layer against the full Goja test suite using a
  hybrid PCRE2 / UTF-16 fallback adapter, including production-bundle regexes.
