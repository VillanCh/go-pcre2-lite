package regexp2

import (
	"reflect"
	"testing"
)

type goldenCapture struct {
	text   string
	index  int
	length int
	set    bool
}

func collectGoldenMatches(t *testing.T, re *Regexp, input string) [][]goldenCapture {
	t.Helper()
	var matches [][]goldenCapture
	m, err := re.FindStringMatch(input)
	if err != nil {
		t.Fatal(err)
	}
	for m != nil {
		var captures []goldenCapture
		for _, group := range m.Groups() {
			captures = append(captures, goldenCapture{
				text:   group.String(),
				index:  group.Index,
				length: group.Length,
				set:    len(group.Captures) > 0,
			})
		}
		matches = append(matches, captures)
		m, err = re.FindNextMatch(m)
		if err != nil {
			t.Fatal(err)
		}
	}
	return matches
}

func TestIterationAndCaptureGoldenResults(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		options RegexOptions
		input   string
		want    [][]goldenCapture
	}{
		{
			name: "global digits", pattern: `(\d+)`, input: "a1 b22 c333",
			want: [][]goldenCapture{
				{{"1", 1, 1, true}, {"1", 1, 1, true}},
				{{"22", 4, 2, true}, {"22", 4, 2, true}},
				{{"333", 8, 3, true}, {"333", 8, 3, true}},
			},
		},
		{
			name: "optional unset", pattern: `(a)?(b)`, input: "b ab",
			want: [][]goldenCapture{
				{{"b", 0, 1, true}, {"", 0, 0, false}, {"b", 0, 1, true}},
				{{"ab", 2, 2, true}, {"a", 2, 1, true}, {"b", 3, 1, true}},
			},
		},
		{
			name: "unicode rune offsets", pattern: `(\p{Han})`, input: "a你好",
			want: [][]goldenCapture{
				{{"你", 1, 1, true}, {"你", 1, 1, true}},
				{{"好", 2, 1, true}, {"好", 2, 1, true}},
			},
		},
		{
			name: "multiline", pattern: `^(\w+)`, options: Multiline, input: "one\ntwo",
			want: [][]goldenCapture{
				{{"one", 0, 3, true}, {"one", 0, 3, true}},
				{{"two", 4, 3, true}, {"two", 4, 3, true}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			re := MustCompile(test.pattern, test.options)
			if got := collectGoldenMatches(t, re, test.input); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestReplaceGoldenResults(t *testing.T) {
	tests := []struct {
		name, pattern, input, replacement, want string
		options                                 RegexOptions
		count                                   int
	}{
		{"simple", `\d+`, "a1 b22 c333", "#", "a# b# c#", 0, -1},
		{"group refs", `(\w+)@(\w+)`, "me@host you@there", "$2.$1", "host.me there.you", 0, -1},
		{"named refs", `(?<k>\w+)=(?<v>\w+)`, "a=1;b=2", "${v}:${k}", "1:a;2:b", 0, -1},
		{"whole match", `\d+`, "x9y", "[$&]", "x[9]y", 0, -1},
		{"literal dollar", `a`, "banana", "$$", "b$n$n$", 0, -1},
		{"left and right", `b`, "abc", "<$`|$'>", "a<a|c>c", 0, -1},
		{"count limit", `o`, "ooooo", "0", "00ooo", 0, 2},
		{"unicode", `(\p{Han})`, "你好", "[$1]", "[你][好]", 0, -1},
		{"ignore case", `a`, "AaA", "_", "___", IgnoreCase, -1},
		{"empty matches", `x*`, "abc", "-", "-a-b-c-", 0, -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MustCompile(test.pattern, test.options).Replace(test.input, test.replacement, -1, test.count)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestEscapeAndUnescapeGoldenResults(t *testing.T) {
	for input, want := range map[string]string{
		"abc":     "abc",
		"a.b*c":   `a\.b\*c`,
		"(group)": `\(group\)`,
		"[set]":   `\[set\]`,
	} {
		if got := Escape(input); got != want {
			t.Errorf("Escape(%q)=%q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		`a\.b`:        "a.b",
		`\x41\x42`:    "AB",
		`\u0041`:      "A",
		`\n\r\t`:      "\n\r\t",
		`back\\slash`: `back\slash`,
	} {
		got, err := Unescape(input)
		if err != nil {
			t.Errorf("Unescape(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("Unescape(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestNamedGroupAndStartingAtGoldenResults(t *testing.T) {
	re := MustCompile(`(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})`, 0)
	m, err := re.FindStringMatch("date 2024-06-22")
	if err != nil || m == nil {
		t.Fatalf("match=%v err=%v", m, err)
	}
	for name, want := range map[string]string{"year": "2024", "month": "06", "day": "22"} {
		group := m.GroupByName(name)
		if group == nil || group.String() != want {
			t.Errorf("group %q=%v, want %q", name, group, want)
		}
		if number := re.GroupNumberFromName(name); number < 1 || m.GroupByNumber(number).String() != want {
			t.Errorf("number lookup for %q failed", name)
		}
	}

	words := MustCompile(`\w+`, 0)
	for _, test := range []struct {
		start, index int
		want         string
	}{
		{0, 0, "abc"},
		{1, 1, "bc"},
		{4, 4, "def"},
		{7, 8, "ghi"},
	} {
		match, matchErr := words.FindStringMatchStartingAt("abc def ghi", test.start)
		if matchErr != nil || match == nil || match.String() != test.want || match.Index != test.index {
			t.Errorf("start=%d match=%v err=%v, want %q at %d", test.start, match, matchErr, test.want, test.index)
		}
	}
}
