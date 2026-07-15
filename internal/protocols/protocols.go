package protocols

import (
	"fmt"
	"strings"
)

const (
	BaseFlooding           = "base_flooding"
	DuplicateAwareFlooding = "duplicate_aware_flooding"
	IDontWantFlooding      = "idontwant_flooding"
)

func Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return BaseFlooding
	}
	return value
}

func Validate(value string) error {
	switch Normalize(value) {
	case BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding:
		return nil
	default:
		return fmt.Errorf("unsupported protocol %q; supported protocols are %s, %s, and %s",
			value, BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding)
	}
}

func UsesDuplicateNeighborSuppression(value string) bool {
	return Normalize(value) == DuplicateAwareFlooding
}

func UsesIDontWant(value string) bool {
	return Normalize(value) == IDontWantFlooding
}
