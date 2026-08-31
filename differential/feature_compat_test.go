package pcre2lite_test

import (
	"testing"

	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

// These tests verify feature-level API parity with dlclark/regexp2 across the
// full match iteration and every capture group -- the corpus test only checks
// the first whole match. We compare match position, length, text, group names
// and named-group lookups. We deliberately do NOT compare Group.Captures slice
// lengths: dlclark records the full capture history of a repeated group while
// PCRE2 reports only the final capture (documented in MIGRATION.md).

// compareMatchAll iterates all matches of both engines over input and asserts
// each match and each of its groups agree.
func compareMatchAll(t *testing.T, name string, dre *dl.Regexp, p2re *p2.Regexp, input string) {
	t.Helper()
	dm, derr := dre.FindStringMatch(input)
	pm, perr := p2re.FindStringMatch(input)
	if derr != nil || perr != nil {
		t.Fatalf("[%s] find error dl=%v p2=%v", name, derr, perr)
	}

	idx := 0
	for {
		if (dm == nil) != (pm == nil) {
			t.Errorf("[%s] match #%d presence mismatch: dl=%v p2=%v", name, idx, dm != nil, pm != nil)
			return
		}
		if dm == nil {
			return
		}

		if dm.Index != pm.Index || dm.Length != pm.Length || dm.String() != pm.String() {
			t.Errorf("[%s] match #%d: dl{idx=%d len=%d %q} != p2{idx=%d len=%d %q}",
				name, idx, dm.Index, dm.Length, dm.String(), pm.Index, pm.Length, pm.String())
		}

		dgs := dm.Groups()
		pgs := pm.Groups()
		if len(dgs) != len(pgs) {
			t.Errorf("[%s] match #%d group count: dl=%d p2=%d", name, idx, len(dgs), len(pgs))
		} else {
			for g := range dgs {
				dg, pg := dgs[g], pgs[g]
				if dg.Name != pg.Name {
					t.Errorf("[%s] match #%d group %d name: dl=%q p2=%q", name, idx, g, dg.Name, pg.Name)
				}
				// A group that participated: compare position/text. dlclark and
				// PCRE2 both report the LAST capture in the embedded Capture.
				dpart := len(dg.Captures) > 0
				ppart := len(pg.Captures) > 0
				if dpart != ppart {
					t.Errorf("[%s] match #%d group %d participation: dl=%v p2=%v",
						name, idx, g, dpart, ppart)
					continue
				}
				if !dpart {
					continue
				}
				if dg.Index != pg.Index || dg.Length != pg.Length || dg.String() != pg.String() {
					t.Errorf("[%s] match #%d group %d: dl{idx=%d len=%d %q} != p2{idx=%d len=%d %q}",
						name, idx, g, dg.Index, dg.Length, dg.String(), pg.Index, pg.Length, pg.String())
				}
			}
		}

		var e1, e2 error
		dm, e1 = dre.FindNextMatch(dm)
		pm, e2 = p2re.FindNextMatch(pm)
		if e1 != nil || e2 != nil {
			t.Fatalf("[%s] FindNextMatch error dl=%v p2=%v", name, e1, e2)
		}
		idx++
		if idx > 100000 {
			t.Fatalf("[%s] runaway iteration", name)
			return
		}
	}
}

func TestFeatureParityGroups(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		opts    p2.RegexOptions
		input   string
	}{
		{"multi-group-date", `(\d{4})-(\d{2})-(\d{2})`, 0, "born 2024-06-22 today"},
		{"named-groups", `(?<y>\d+)/(?<m>\d+)/(?<d>\d+)`, 0, "2024/06/22"},
		{"global-words", `\w+`, 0, "foo bar baz qux"},
		{"global-digits", `\d+`, 0, "a1b22c333d4444"},
		{"optional-unset", `(a)?(b)`, 0, "b then ab"},
		{"nested-groups", `((a)(b))+`, 0, "ababab"},
		{"alternation", `(cat|dog)s?`, 0, "cats and dogs and cat"},
		{"lookahead", `\d+(?= dollars)`, 0, "pay 100 dollars now"},
		{"lookbehind", `(?<=\$)\d+`, 0, "costs $100 and $250"},
		{"neg-lookahead", `foo(?!bar)`, 0, "foobar foobaz foo"},
		{"backref", `(\w+) \1`, 0, "hello hello world world"},
		{"empty-capable", `a*`, 0, "baaabaa"},
		{"anchored-multiline", `^\w+`, p2.Multiline, "one\ntwo\nthree"},
		{"ignorecase", `[a-z]+`, p2.IgnoreCase, "Hello WORLD"},
		{"dotstar-greedy", `<(.+)>`, 0, "<a><b><c>"},
		{"unicode-word", `\w+`, 0, "café münchen"},
		{"mixed-named-unnamed", `(\d+)(?<unit>[a-z]+)`, 0, "10kg 25m 100ml"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dre, derr := dl.Compile(tc.pattern, dl.RegexOptions(tc.opts))
			p2re, perr := p2.Compile(tc.pattern, tc.opts)
			if (derr == nil) != (perr == nil) {
				t.Fatalf("compile parity: dl=%v p2=%v", derr, perr)
			}
			if derr != nil {
				return
			}
			compareMatchAll(t, tc.name, dre, p2re, tc.input)
		})
	}
}

// TestFeatureNamedGroupLookup verifies GroupByName / GroupByNumber parity.
func TestFeatureNamedGroupLookup(t *testing.T) {
	pattern := `(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})`
	input := "2024-06-22"

	dre := dl.MustCompile(pattern, 0)
	p2re := p2.MustCompile(pattern, 0)

	dm, _ := dre.FindStringMatch(input)
	pm, _ := p2re.FindStringMatch(input)
	if dm == nil || pm == nil {
		t.Fatalf("expected match: dl=%v p2=%v", dm != nil, pm != nil)
	}

	for _, gname := range []string{"year", "month", "day", "missing"} {
		dg := dm.GroupByName(gname)
		pg := pm.GroupByName(gname)
		if (dg == nil) != (pg == nil) {
			t.Errorf("group %q presence: dl=%v p2=%v", gname, dg != nil, pg != nil)
			continue
		}
		if dg == nil {
			continue
		}
		if dg.String() != pg.String() {
			t.Errorf("group %q: dl=%q p2=%q", gname, dg.String(), pg.String())
		}
	}

	// Numbered lookups, including out of range.
	for n := 0; n <= 4; n++ {
		dg := dm.GroupByNumber(n)
		pg := pm.GroupByNumber(n)
		if (dg == nil) != (pg == nil) {
			t.Errorf("group #%d presence: dl=%v p2=%v", n, dg != nil, pg != nil)
			continue
		}
		if dg != nil && dg.String() != pg.String() {
			t.Errorf("group #%d: dl=%q p2=%q", n, dg.String(), pg.String())
		}
	}
}

// TestFeatureGroupNumberFromName verifies name->number mapping parity for the
// common cases (all-named or all-unnamed groups), where .NET and PCRE numbering
// agree.
func TestFeatureGroupNumberFromName(t *testing.T) {
	cases := []struct {
		pattern string
		names   []string
	}{
		{`(?<a>x)(?<b>y)(?<c>z)`, []string{"a", "b", "c", "nope"}},
		{`(?<year>\d+)-(?<month>\d+)`, []string{"year", "month", "day"}},
		{`(?<first>\w+)\s+(?<second>\w+)`, []string{"first", "second"}},
	}
	for _, tc := range cases {
		dre := dl.MustCompile(tc.pattern, 0)
		p2re := p2.MustCompile(tc.pattern, 0)
		for _, name := range tc.names {
			if dre.GroupNumberFromName(name) != p2re.GroupNumberFromName(name) {
				t.Errorf("pattern %q GroupNumberFromName(%q): dl=%d p2=%d",
					tc.pattern, name, dre.GroupNumberFromName(name), p2re.GroupNumberFromName(name))
			}
		}
	}
}

// TestFeatureMixedGroupNumberingDocumented documents the one intentional
// numbering divergence: with MIXED named and unnamed groups, dlclark mimics
// .NET (unnamed groups numbered first, then named) while PCRE2 numbers strictly
// by left-to-right appearance. Access BY NAME returns identical content in both
// engines -- only the integer index differs. See MIGRATION.md.
func TestFeatureMixedGroupNumberingDocumented(t *testing.T) {
	pattern := `(?<a>x)(y)(?<b>z)`
	input := "xyz"
	dre := dl.MustCompile(pattern, 0)
	p2re := p2.MustCompile(pattern, 0)

	dlA := dre.GroupNumberFromName("a")
	p2A := p2re.GroupNumberFromName("a")
	t.Logf("mixed-group numbering of %q: dl(.NET)=%d p2(PCRE)=%d", "a", dlA, p2A)

	// The practical compatibility guarantee: access by name yields the same text.
	dm, _ := dre.FindStringMatch(input)
	pm, _ := p2re.FindStringMatch(input)
	if dm == nil || pm == nil {
		t.Fatalf("expected match")
	}
	for _, name := range []string{"a", "b"} {
		dg, pg := dm.GroupByName(name), pm.GroupByName(name)
		if dg == nil || pg == nil {
			t.Fatalf("group %q missing: dl=%v p2=%v", name, dg != nil, pg != nil)
		}
		if dg.String() != pg.String() {
			t.Errorf("by-name content %q: dl=%q p2=%q", name, dg.String(), pg.String())
		}
	}
}

// TestFeatureCaptureHistoryDocumented documents (rather than asserts equality
// of) the one intentional divergence: repeated-group capture history. dlclark
// keeps every iteration; PCRE2 keeps the last. We assert the LAST capture
// matches, which is what the embedded Group/Capture exposes.
func TestFeatureCaptureHistoryDocumented(t *testing.T) {
	pattern := `(\d)+`
	input := "12345"

	dre := dl.MustCompile(pattern, 0)
	p2re := p2.MustCompile(pattern, 0)
	dm, _ := dre.FindStringMatch(input)
	pm, _ := p2re.FindStringMatch(input)

	dg := dm.GroupByNumber(1)
	pg := pm.GroupByNumber(1)
	if dg.String() != pg.String() {
		t.Errorf("last capture of repeated group: dl=%q p2=%q", dg.String(), pg.String())
	}
	t.Logf("capture history lengths (informational): dl=%d p2=%d",
		len(dg.Captures), len(pg.Captures))
	if dg.String() != "5" || pg.String() != "5" {
		t.Errorf("expected last capture %q, got dl=%q p2=%q", "5", dg.String(), pg.String())
	}
}
