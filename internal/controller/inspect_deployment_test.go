package controller

import "testing"

func TestIPv4CIDRCapacity(t *testing.T) {
	tests := map[string]int64{
		"10.0.0.0/24": 256,
		"10.0.0.0/21": 2048,
		"10.0.0.0/16": 65536,
	}
	for cidr, expected := range tests {
		actual, err := ipv4CIDRCapacity(cidr)
		if err != nil {
			t.Fatalf("%s: %v", cidr, err)
		}
		if actual != expected {
			t.Fatalf("%s: got %d, want %d", cidr, actual, expected)
		}
	}
}

func TestInspectionSeverityOrder(t *testing.T) {
	if got := maxInspectionSeverity(InspectionOK, InspectionWarning); got != InspectionWarning {
		t.Fatalf("got %s", got)
	}
	if got := maxInspectionSeverity(InspectionWarning, InspectionError); got != InspectionError {
		t.Fatalf("got %s", got)
	}
}
