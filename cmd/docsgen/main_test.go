package main

import "testing"

func TestMDXEscapesOpeningDelimitersOnly(t *testing.T) {
	if got := mdx("pass <ref> and {x}"); got != `pass \<ref> and \{x}` {
		t.Fatal(got)
	}
	if got := cell("a|b\nc"); got != `a\|b c` {
		t.Fatal(got)
	}
}
