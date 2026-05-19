package twilioadapter

import (
	"errors"
	"strings"
)

// IsAllowedCountry returns true if the E.164 phone number's country-code
// prefix matches any entry in allowed. Each entry is an E.164 country
// prefix (e.g. "+1", "+44", "+91"). Longest match wins. Empty allowed
// list returns false (deny-by-default).
//
// Phone must be E.164 (must start with "+"). Otherwise returns false.
func IsAllowedCountry(phone string, allowed []string) bool {
	if !strings.HasPrefix(phone, "+") {
		return false
	}
	if len(allowed) == 0 {
		return false
	}
	best := ""
	for _, prefix := range allowed {
		if strings.HasPrefix(phone, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	return best != ""
}

// ParseAllowedCountryCodes parses a comma-separated CLI input into a
// validated slice of country-code prefixes. Each token is trimmed.
// Tokens that don't start with "+" get a "+" prefix added. Returns
// the cleaned slice and an error if any token is empty or non-digit
// after the "+".
func ParseAllowedCountryCodes(csv string) ([]string, error) {
	if strings.TrimSpace(csv) == "" {
		return nil, errors.New("country code list empty")
	}
	tokens := strings.Split(csv, ",")
	result := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, errors.New("country code list empty")
		}
		if !strings.HasPrefix(tok, "+") {
			tok = "+" + tok
		}
		digits := tok[1:]
		if digits == "" {
			return nil, errors.New("country code must be digits after +")
		}
		for _, c := range digits {
			if c < '0' || c > '9' {
				return nil, errors.New("country code must be digits after +")
			}
		}
		result = append(result, tok)
	}
	return result, nil
}
