package config

import (
	"reflect"
	"testing"
)

func TestJitterYAMLFieldNames(t *testing.T) {
	nodeField, ok := reflect.TypeOf(NodePerformance{}).FieldByName("ProcessingDelayJitterStdDevMS")
	if !ok {
		t.Fatal("ProcessingDelayJitterStdDevMS field is missing")
	}
	if got := nodeField.Tag.Get("yaml"); got != "processing_delay_jitter_stddev,omitempty" {
		t.Fatalf("node jitter YAML tag=%q", got)
	}

	edgeField, ok := reflect.TypeOf(EdgeNetwork{}).FieldByName("DelayJitterStdDevMS")
	if !ok {
		t.Fatal("DelayJitterStdDevMS field is missing")
	}
	if got := edgeField.Tag.Get("yaml"); got != "delay_jitter_stddev,omitempty" {
		t.Fatalf("edge jitter YAML tag=%q", got)
	}
}
