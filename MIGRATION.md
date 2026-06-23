# Migrating from `github.com/dlclark/regexp2`

`go-pcre2-lite/regexp2` is a drop-in replacement for `github.com/dlclark/regexp2`.
For the overwhelming majority of code, migration is a one-line change:

```go
// before
import "github.com/dlclark/regexp2"

// after
import regexp2 "github.com/VillanCh/go-pcre2-lite/regexp2"
```

The exported API (types, methods, `RegexOptions` constants, rune-based
`Index`/`Length`) mirrors `regexp2`. Match positions are reported as **rune
indices**, exactly like `regexp2`, even though the engine runs on UTF-8 bytes.

`CGO_ENABLED=1` is required: the engine is a trimmed PCRE2 8-bit interpreter
compiled from vendored C source, with JIT permanently disabled.

## What is verified identical

These are covered by the differential and corpus test suites in this repo:

- Whole-match results across **1585 inputs / 764 patterns** from the PCRE2
  official `testoutput1` corpus: **100% agreement** with `dlclark/regexp2`.
- Behaviour against **PCRE2 10.47's own official regression corpora**
  (`testoutput2` for features/boundaries, `testoutput4` for UTF/Unicode
  properties): match results agree on **929/931** 8-bit cases and **1502/1504**
  UTF cases; compile accept/reject agrees on **1258/1258** accepted and
  **385/388** rejected patterns. This pins the engine to upstream ground truth,
  not just to `dlclark`. (The few misses are boundary cases the lightweight
  corpus parser approximates, not engine bugs.)
- Capture groups, named groups, lookahead/lookbehind, backreferences, anchors,
  alternation, greedy/lazy quantifiers, multiline, case-insensitive, Unicode
  `\w`/`\d`/`\p{...}` classes.
- `Replace`, `ReplaceFunc`, `Escape`, `Unescape`, `FindStringMatch`,
  `FindNextMatch`, `FindStringMatchStartingAt`, `GroupByName`, `GroupByNumber`.
- Access **by name** (`GroupByName`) always returns identical content.

## Documented differences

These are intentional and stem from the .NET-vs-PCRE engine difference. Each has
a dedicated test that documents the behaviour.

### 1. ReDoS is bounded by default (safety improvement)

`dlclark/regexp2` defaults to `MatchTimeout = NoTimeout` ("forever"); a
catastrophic pattern/input pair can hang indefinitely until you set a timeout.

This library is bounded by the PCRE2 **match limit** (default 10,000,000 steps,
~120 ms worst case) and **depth limit**, after which the match returns
`ErrMatchLimit` / `ErrDepthLimit` instead of hanging. PCRE2 10.x uses heap-based
matching, so deep patterns do not overflow the C stack.

- `MatchTimeout` is accepted for API compatibility but does **not** enforce a
  wall-clock abort.
- Use `(*Regexp).SetMatchLimits(matchLimit, depthLimit uint32)` (an extension)
  to tighten or relax the budget. A tight limit (e.g. 50,000) turns a runaway
  pattern into a sub-millisecond `ErrMatchLimit`.

```go
re := regexp2.MustCompile(userPattern, 0)
_ = re.SetMatchLimits(100000, 100000) // fail fast on adversarial input
```

### 2. Group numbering with MIXED named and unnamed groups

.NET (and therefore `dlclark/regexp2`) renumbers groups so that **unnamed groups
come first, then named groups**. PCRE2 numbers strictly by **left-to-right
appearance**.

```
pattern: (?<a>x)(y)(?<b>z)
dlclark (.NET):  a=2, (y)=1, b=3
pcre2-lite:      a=1, (y)=2, b=3
```

Impact: only the **integer index** differs. `GroupByName("a")` returns the same
text in both. If you reference mixed groups by number, switch to names. Patterns
that are all-named or all-unnamed number identically in both engines.

### 3. `GroupNumberFromName` with a numeric string

For an all-named pattern, `dlclark` treats `GroupNumberFromName("1")` as a name
lookup ("is there a group literally named `1`?") and returns `-1`. This library
may interpret a numeric string as an index. Prefer real names.

### 4. Repeated-group capture history

For a repeated capturing group, `dlclark` records the **full capture history** in
`Group.Captures`; PCRE2 records only the **final** capture.

```
pattern: (\d)+   input: 12345
dlclark:    Group(1).Captures = [1 2 3 4 5]
pcre2-lite: Group(1).Captures = [5]
```

The embedded `Group`/`Capture` (the last capture) is identical in both:
`Group(1).String() == "5"`.

### 5. `RightToLeft`

The `RightToLeft` option is accepted and `(*Regexp).RightToLeft()` reports it,
but the engine always scans left-to-right. Right-to-left scanning semantics are
not reproduced.

### 6. Character-class edge cases

PCRE2 is stricter than .NET for a few constructs, e.g. `[\d-z]` (a range whose
start is a class shorthand) is rejected by PCRE2 but accepted by .NET. Conversely
PCRE2 accepts many constructs .NET does not (atomic groups, possessive
quantifiers, recursion, `\K`, subroutine calls), so some patterns that fail to
compile under `dlclark` compile here.

The shorthand escapes `\h`/`\H` (horizontal whitespace) and `\v`/`\V` (vertical
whitespace) follow **PCRE2** semantics, which differ from .NET: in .NET `\v`
matches only the vertical tab `U+000B`, whereas in PCRE2 `\v` is the vertical
whitespace class (it also matches `\n`, `\r`, `\f` and `U+0085`/`U+2028`/`U+2029`
in Unicode mode). A pattern like `\v` against `"\r"` therefore matches here but
not under `dlclark`. This is an inherent engine difference, not a porting bug.

### 7. Invalid UTF-8 input

The compat layer normalises invalid UTF-8 in the **subject** to the Unicode
replacement character (U+FFFD) before matching, so it never errors on bad input
(rune-oriented, like `regexp2`). The low-level byte API returns `ErrBadUTF` in
UTF mode instead.

## JavaScript / Node.js regex portability

This engine is also a practical target for porting JavaScript/Node regexes.
`js_regex_test.go` checks behaviour against authoritative JS sources (ECMAScript
`test262` lookbehind/named-group cases, the V8 Unicode-property blog examples)
and against real-world ReDoS CVEs. What ports cleanly and what does not:

**Ports identically to JS:**

- Fixed-length lookbehind with captures: `(?<=(\w(\w)))def`, `(?<=(bc)|(cd)).`.
- Variable-length lookbehind, e.g. `(?<=[ab]+)x`, `(?<="text":\s*")`: PCRE2 10.47
  supports bounded variable-length lookbehind natively, and the compat layer
  (`compat.go`) tightens otherwise-unbounded quantifiers (`*` `+` `{n,}`) inside a
  lookbehind to a bounded form (`{0,512}` etc.) so they compile and match. The
  only difference from JS/.NET is that more than 512 repetitions inside the
  lookbehind are not matched (a generous, configurable bound).
- Named groups and `\k<name>` backreferences: `(?<year>\d{4})-(?<month>\d{2})`.
- Unicode property escapes via the **short** names: `\p{N}`, `\p{L}`, and binary
  properties `\p{Alphabetic}`, `\p{Math}` (the compat layer enables UTF+UCP).
- Character classes where a set shorthand neighbours `-`, e.g. `[\d\w-_]`: the
  compat layer treats the `-` as a literal (as .NET/RE2 do), avoiding PCRE2's
  "invalid range in character class" error.
- Global iteration over successive matches (`FindNextMatch`).

**Documented JS-vs-PCRE2 divergences** (each has a dedicated test):

| JS construct | JS behaviour | PCRE2 10.47 behaviour |
|---|---|---|
| Lookbehind whose length depends on a backreference, e.g. `(?<=a(.\2)b(\1))` | accepted | **compile error** (length not boundable) |
| Long `General_Category` name `\p{Number}` | accepted | **compile error** — use the short alias `\p{N}` |
| Quantified capture in lookbehind `(?<=(\w){3})def` | group 1 = `"a"` (matched right-to-left) | group 1 = `"c"` (whole match `"def"` agrees) |
| Backreference inside lookbehind `(?<=\1(\w))d` | matches | compiles but **does not match** |

**ReDoS / security:** every real-world JS evil regex (moment.js
CVE-2022-31129, the Cloudflare-2019 rule, CWE-1333, the classic catastrophic
email matcher, UAParser.js CVE-2020-7733) **terminates** under the default
limits — the engine never hangs. For genuinely *exponential* patterns a tight
`SetMatchLimits` collapses the attack into a sub-millisecond `ErrMatchLimit`.
Note that `match_limit` bounds *exponential* backtracking but **not** a
*quadratic* scan (moment's hotspot is O(n^2)); for polynomial patterns the only
effective defense is capping input length.

## Performance

Measured on Apple M-series (`go test -bench`), `p2` = this compat layer:

Measured against vendored PCRE2 10.47:

| Scenario              | dlclark            | pcre2-lite       | Speedup | Alloc |
|-----------------------|--------------------|------------------|---------|-------|
| Boolean match (short) | 6472 ns, 224 B     | 676 ns, 0 B      | 9.6x    | 0     |
| 100 KB single match   | 26 ms, 418 KB      | 2.84 ms, 0 B     | 9.3x    | 0     |
| Backreference         | 396 ns, 144 B      | 186 ns, 0 B      | 2.1x    | 0     |
| Backtracking-heavy    | 20 ms, 131 KB      | 10.9 ms, 0 B     | 1.8x    | 0     |
| ReDoS (default limit) | hangs w/o timeout  | 120 ms (bounded) | --      | 0     |
| ReDoS (limit 50k)     | --                 | 0.6 ms           | --      | 0     |

Boolean matching is allocation-free on the hot path.

## How compatibility is verified

- `corpus_pcre_test.go`   — 1585 inputs from PCRE2 `testoutput1`, dl-vs-p2.
- `pcre2_official_test.go` — PCRE2 10.47 own corpora (`testoutput2` features/
  boundaries, `testoutput4` UTF/properties): match + compile accept/reject vs
  upstream ground truth.
- `js_regex_test.go`      — JS/Node compatibility (test262/V8 lookbehind, named
  groups, Unicode properties) and real-world ReDoS CVE safety.
- `differential_test.go`, `differential_replace_test.go` — core + replace parity.
- `feature_compat_test.go` — full-iteration, all-group, named-group parity.
- `safety_test.go`        — ReDoS bounding, invalid UTF-8, huge input, deep nesting.
- `pcre2_1047_regression_test.go` — per-version behaviour pins for 10.44–10.47
  (variable-length lookbehind first-branch fix, `\X` ZWJ grapheme break, scan-
  substring `(*scs:)`/`(*scan_substring:)`, `(*ACCEPT)`-in-scs CVE-2025-58050
  memory safety, 10.47 subroutine-returning-captures `(?N(group,...))`,
  `pcre2_next_match` `\K`/empty-match iteration, UCD 16 properties, group-name
  128-char limit boundary, and the named-group hash-lookup guard). Every case is
  asserted against the authoritative `pcre2test` golden output for that release.
- `stress_test.go`        — 20k+ random adversarial patterns, all bounded.
- `benchmark*_test.go`    — throughput/allocation vs dlclark and std `regexp`.
