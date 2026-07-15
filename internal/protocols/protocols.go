package protocols

import "strings"

const (
	BaseFlooding           = "base_flooding"
	DuplicateAwareFlooding = "duplicate_aware_flooding"
)

func Normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func IsSupported(name string) bool {
	switch Normalize(name) {
	case BaseFlooding, DuplicateAwareFlooding:
		return true
	default:
		return false
	}
}
