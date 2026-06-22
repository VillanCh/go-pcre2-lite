# Test data

All files originate from the PCRE2 project (https://www.pcre.org/); PCRE2 is
under the BSD 3-Clause License. Included unmodified, for testing only.

## pcre2_testoutput1.txt

`testoutput1` from PCRE2 v10.21 — the reference output of the PCRE2 test suite.

It is used here **only** as a corpus of real-world patterns and inputs for
differential testing (see `../corpus_pcre_test.go`): every pattern/input pair is
run through both `github.com/dlclark/regexp2` and this library and the results
are compared. The file's own recorded expectations are not used as an oracle
(they encode non-UTF 8-bit semantics).

- Redistributed via `github.com/dlclark/regexp2`, which documents it as public
  domain.

## pcre2_testoutput2.txt / pcre2_testoutput4.txt

`testoutput2` (features, boundaries, compile diagnostics, 8-bit/non-UTF) and
`testoutput4` (UTF and Unicode-property matching) from PCRE2 v10.42.

Unlike `testoutput1`, these are used as an **oracle**: `../pcre2_official_test.go`
parses the recorded compile accept/reject outcomes and per-subject match results
and checks this library's engine directly against PCRE2's own ground truth. The
parser skips cases that rely on `pcre2test`-only features (callouts, subject
repeats, exotic per-subject modifiers).
