# Test data

All files originate from the PCRE2 project (https://www.pcre.org/); PCRE2 is
under the BSD 3-Clause License. Included unmodified, for testing only.

## pcre2_testoutput2.txt / pcre2_testoutput4.txt

`testoutput2` (features, boundaries, compile diagnostics, 8-bit/non-UTF) and
`testoutput4` (UTF and Unicode-property matching) from PCRE2 v10.47, matching
the vendored engine version (see `../internal/pcre2lite/upstream-version.txt`).

These are used as an **oracle**: `../pcre2_official_test.go`
parses the recorded compile accept/reject outcomes and per-subject match results
and checks this library's engine directly against PCRE2's own ground truth. The
parser skips cases that rely on `pcre2test`-only features (callouts, subject
repeats, exotic per-subject modifiers).
