package bridgeadmin

import "testing"

func TestSuggestAlias(t *testing.T) {
	cases := map[string]string{
		"TLDR":                  "tldr",
		"DW Berlin Briefing":    "dw-berlin-briefing",
		"Tim Ferriss' 5-Bullet": "tim-ferriss-5-bullet",
		"  Hacker  Newsletter ": "hacker-newsletter",
		"Café Größe":            "cafe-grosse",
		"!!!":                   "",
	}
	for name, want := range cases {
		if got := SuggestAlias(name); got != want {
			t.Errorf("SuggestAlias(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidateAlias(t *testing.T) {
	valid := []string{"tldr", "dw-berlin-briefing", "a1"}
	for _, alias := range valid {
		if err := ValidateAlias(alias); err != nil {
			t.Errorf("ValidateAlias(%q) = %v, want nil", alias, err)
		}
	}
	invalid := []string{"", "TLDR", "has space", "under_score", "dot.ted", "ü", "-leading", "trailing-"}
	for _, alias := range invalid {
		if err := ValidateAlias(alias); err == nil {
			t.Errorf("ValidateAlias(%q) = nil, want an error", alias)
		}
	}
}

func TestHashSecretMatchesTheWorkersVerifySecret(t *testing.T) {
	// sha256("abc") in lowercase hex, the FIPS 180-2 test vector. It is what
	// email-worker/src/basicAuth.ts hashSecret produces for "abc".
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := HashSecret("abc"); got != want {
		t.Errorf("HashSecret(abc) = %s, want %s", got, want)
	}
}

func TestNewSecretIs48HexCharsAndFresh(t *testing.T) {
	a, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSecret()
	if len(a) != 48 || a == b {
		t.Errorf("NewSecret gave %q and %q; want two different 48-char values", a, b)
	}
	for _, r := range a {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			t.Fatalf("NewSecret %q is not lowercase hex", a)
		}
	}
}
