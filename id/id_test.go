package id

import "testing"

func TestFormat(t *testing.T) {
	cases := map[int64]string{
		1:       "X-000001",
		123:     "X-000123",
		999999:  "X-999999",
		1000000: "X-1000000",
	}
	for in, want := range cases {
		if got := Format(in); got != want {
			t.Errorf("Format(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	ok := map[string]int64{
		"X-000123": 123,
		"x-000123": 123,
		"X-123":    123,
		"X123":     123,
		"123":      123,
		"  X-42  ": 42,
		"1":        1,
	}
	for in, want := range ok {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %d, want %d", in, got, want)
		}
	}

	bad := []string{"", "   ", "abc", "X-", "X", "0", "X-0", "-5", "12.3", "X-12x"}
	for _, in := range bad {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", in)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, n := range []int64{1, 9, 123, 999999, 1000000, 123456789} {
		got, err := Parse(Format(n))
		if err != nil {
			t.Fatalf("round-trip Parse(Format(%d)): %v", n, err)
		}
		if got != n {
			t.Errorf("round-trip %d -> %q -> %d", n, Format(n), got)
		}
	}
}
