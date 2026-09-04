package tools

import (
	"strings"
	"testing"
)

// rawArrayLen guards the O(1)-memory counting path used by the large-array
// tools (candles, fills, funding). It must match rawToSlice's observable
// behavior: same count, JSON null → 0, malformed JSON → error envelope.
func TestRawArrayLen(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    int
		wantErr string // substring; empty means success
	}{
		{"empty array", `[]`, 0, ""},
		{"null body", `null`, 0, ""},
		{"null with whitespace", " \n null \n", 0, ""},
		{"scalars", `[1, 2.5, "x", true, null]`, 5, ""},
		{"objects", `[{"a":1},{"a":2,"b":{"c":[1,2,3]}}]`, 2, ""},
		{"nested arrays", `[[1,2],[3,[4,5]],[]]`, 3, ""},
		{"string with bracket", `["]", "{"]`, 2, ""},
		{"whitespace heavy", "[ \n { \"a\": 1 } , \t 5 \n ]", 2, ""},
		{"empty body", ``, 0, "failed to parse API response"},
		{"object body", `{"a":1}`, 0, "expected JSON array"},
		{"unterminated", `[{"a":1},`, 0, "failed to parse API response"},
		{"garbage element", `[1, @]`, 0, "failed to parse API response"},
		{"trailing garbage", `[1] extra`, 0, "failed to parse API response"},
		{"trailing array", `[1] [2]`, 0, "unexpected data after array"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rawArrayLen([]byte(tc.body))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("rawArrayLen(%q) = %d, %v; want error containing %q", tc.body, got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("rawArrayLen(%q): %v", tc.body, err)
			}
			if got != tc.want {
				t.Errorf("rawArrayLen(%q) = %d, want %d", tc.body, got, tc.want)
			}
			// Cross-check against the materializing decoder on valid inputs.
			s, serr := rawToSlice([]byte(tc.body))
			if serr != nil {
				t.Fatalf("rawToSlice(%q) disagrees on validity: %v", tc.body, serr)
			}
			if len(s) != got {
				t.Errorf("rawArrayLen(%q) = %d but rawToSlice has %d elements", tc.body, got, len(s))
			}
		})
	}
}
