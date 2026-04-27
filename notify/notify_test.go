package notify

import "testing"

func TestAppleScriptEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`hello`, `hello`},
		{`he said "hi"`, `he said \"hi\"`},
		{`back\slash`, `back\\slash`},
		{`mix "a" \ b`, `mix \"a\" \\ b`},
	}
	for _, c := range cases {
		got := appleScriptEscape(c.in)
		if got != c.want {
			t.Errorf("appleScriptEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
