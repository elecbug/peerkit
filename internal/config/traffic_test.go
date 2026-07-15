package config

import "testing"

func TestIsRandomTrafficSource(t *testing.T) {
	for _, value := range []string{"random", "RANDOM", " random "} {
		if !IsRandomTrafficSource(value) {
			t.Fatalf("%q should be recognized as random", value)
		}
	}
	if IsRandomTrafficSource("n0") {
		t.Fatal("ordinary node id should not be random")
	}
}
