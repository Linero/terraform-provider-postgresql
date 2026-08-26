package provider

import "testing"

func TestQuoteIdent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "test_user", `"test_user"`},
		{"embedded quote", `weird"role`, `"weird""role"`},
		{"empty", "", `""`},
		{"injection attempt", `foo"; DROP TABLE bar; --`, `"foo""; DROP TABLE bar; --"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteIdent(c.in); got != c.want {
				t.Errorf("quoteIdent(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "hunter2", `'hunter2'`},
		{"embedded quote", "it's", `'it''s'`},
		{"empty", "", `''`},
		{"backslash", `a\b`, `E'a\\b'`},
		{"quote and backslash", `a'\b`, `E'a''\\b'`},
		{"injection attempt", "'; DROP TABLE bar; --", `'''; DROP TABLE bar; --'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteLiteral(c.in); got != c.want {
				t.Errorf("quoteLiteral(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
