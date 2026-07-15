package protocols

import "testing"

func TestNormalizeDefaultsToBaseFlooding(t *testing.T) {
	if got := Normalize(""); got != BaseFlooding {
		t.Fatalf("Normalize(empty)=%q; want %q", got, BaseFlooding)
	}
}

func TestValidateSupportedProtocols(t *testing.T) {
	for _, value := range []string{BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding} {
		if err := Validate(value); err != nil {
			t.Fatalf("Validate(%q): %v", value, err)
		}
	}
	if err := Validate("unknown"); err == nil {
		t.Fatal("Validate(unknown) succeeded")
	}
}
