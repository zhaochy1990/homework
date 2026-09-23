package subject

import "testing"

func TestValid(t *testing.T) {
	for _, s := range All {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "数 学", "数学课", "English", "电脑"} {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
