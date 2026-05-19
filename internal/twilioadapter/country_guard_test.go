package twilioadapter

import (
	"testing"
)

func TestIsAllowedCountry(t *testing.T) {
	tests := []struct {
		name    string
		phone   string
		allowed []string
		want    bool
	}{
		{"us number allowed +1", "+13102957704", []string{"+1"}, true},
		{"uk number denied +1 only", "+447900900900", []string{"+1"}, false},
		{"uk number allowed +44", "+447900900900", []string{"+44"}, true},
		{"longest match +12 beats +1", "+12025550100", []string{"+1", "+12"}, true},
		{"no leading plus returns false", "13102957704", []string{"+1"}, false},
		{"empty allow list returns false", "+1", []string{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAllowedCountry(tc.phone, tc.allowed)
			if got != tc.want {
				t.Errorf("IsAllowedCountry(%q, %v) = %v; want %v", tc.phone, tc.allowed, got, tc.want)
			}
		})
	}
}

func TestIsAllowedCountry_LongestMatchValue(t *testing.T) {
	// Verify the winning prefix is +12, not +1, by checking a number that
	// starts with +12 but would only match +1 if longest-match is broken.
	phone := "+12025550100"
	allowed := []string{"+1", "+12"}
	if !IsAllowedCountry(phone, allowed) {
		t.Fatalf("expected true for longest-match test")
	}
	// A number starting with +13 must NOT match +12.
	if !IsAllowedCountry("+13102957704", []string{"+1", "+12"}) {
		t.Fatal("+13... should still match +1")
	}
}

func TestParseAllowedCountryCodes(t *testing.T) {
	t.Run("two codes with plus", func(t *testing.T) {
		got, err := ParseAllowedCountryCodes("+1,+44")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0] != "+1" || got[1] != "+44" {
			t.Errorf("got %v; want [+1 +44]", got)
		}
	})

	t.Run("whitespace and missing plus", func(t *testing.T) {
		got, err := ParseAllowedCountryCodes("+1, 44 ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0] != "+1" || got[1] != "+44" {
			t.Errorf("got %v; want [+1 +44]", got)
		}
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := ParseAllowedCountryCodes("")
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("non-digit after plus returns error", func(t *testing.T) {
		_, err := ParseAllowedCountryCodes("+abc")
		if err == nil {
			t.Fatal("expected error for +abc")
		}
	})
}
