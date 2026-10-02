package game

import "testing"

// TestCfmtIntVerbs: C %i with flags, width and precision formats like %d;
// %% and other verbs are untouched.
func TestCfmtIntVerbs(t *testing.T) {
	cases := []struct {
		format string
		args   []any
		want   string
	}{
		{"%i", []any{7}, "7"},
		{"%3i/%3i", []any{4, 14}, "  4/ 14"},
		{"%-4i|", []any{5}, "5   |"},
		{"%03i", []any{9}, "009"},
		{"%.2i", []any{3}, "03"},
		{"100%% %i", []any{1}, "100% 1"},
		{"%%i %i", []any{2}, "%i 2"},
		{"%s %5.1f %i", []any{"hp", 2.25, 3}, "hp   2.2 3"},
		{"no verbs", nil, "no verbs"},
	}
	for _, c := range cases {
		if got := cfmt(c.format, c.args...); got != c.want {
			t.Errorf("cfmt(%q) = %q, want %q", c.format, got, c.want)
		}
	}
}

// TestHelpComputerKillsField: the help layout's "%3i/%3i" counters are
// formatted as C does (they used to come out as %!i(int=...)).
func TestHelpComputerKillsField(t *testing.T) {
	got := cfmt("xv 50 yv 172 string2 \"%3i/%3i     %i/%i       %i/%i\" ", 3, 14, 0, 2, 1, 3)
	want := "xv 50 yv 172 string2 \"  3/ 14     0/2       1/3\" "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
