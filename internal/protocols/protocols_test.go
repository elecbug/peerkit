package protocols

import "testing"

func TestNormalizeAndSupport(t *testing.T) {
	if got := Normalize(" Duplicate_Aware_Flooding "); got != DuplicateAwareFlooding {
		t.Fatalf("Normalize returned %q", got)
	}
	if !IsSupported(BaseFlooding) || !IsSupported(DuplicateAwareFlooding) {
		t.Fatal("known protocols must be supported")
	}
	if IsSupported("unknown") {
		t.Fatal("unknown protocol must not be supported")
	}
}
