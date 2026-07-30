package logins

import "testing"

func TestExtractsTrailingLoginFromPiscineName(t *testing.T) {
	assertEq(t, PiscineLoginFromName("[PISCINE] 249 lgauvrea"), "lgauvrea")
}

func TestCaseInsensitiveTagLowercasesLogin(t *testing.T) {
	assertEq(t, PiscineLoginFromName("[piscine] 12 JDupont"), "jdupont")
}

func TestIgnoresExtraWhitespace(t *testing.T) {
	assertEq(t, PiscineLoginFromName("  [PISCINE]   3    mmartin  "), "mmartin")
}

func TestKeepsHyphensInLogin(t *testing.T) {
	assertEq(t, PiscineLoginFromName("[PISCINE] 013 mdi-boni"), "mdi-boni")
}

func TestReturnsEmptyForNonPiscineNames(t *testing.T) {
	assertEq(t, PiscineLoginFromName("Jean Dupond"), "")
}

func TestReturnsEmptyWhenTrailingTokenIsNotLogin(t *testing.T) {
	assertEq(t, PiscineLoginFromName("[PISCINE] 249"), "")
}

func assertEq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
