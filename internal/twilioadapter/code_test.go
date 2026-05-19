package twilioadapter

import (
	"testing"
	"unicode"
)

// T6: code is exactly 6 numeric digits.

func TestGenerateApprovalCode_T6_SixNumericDigits(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < 1000; i++ {
		code, err := GenerateApprovalCode()
		if err != nil {
			t.Fatalf("GenerateApprovalCode iteration %d: %v", i, err)
		}
		if len(code) != 6 {
			t.Fatalf("code %q length %d, want 6", code, len(code))
		}
		for _, r := range code {
			if !unicode.IsDigit(r) {
				t.Fatalf("non-digit in code %q: %q", code, r)
			}
		}
		seen[code]++
	}
	// crypto/rand should rarely collide across 1000 draws over 1M space;
	// allow a few collisions but flag pathological clustering.
	if seen["000000"] > 5 {
		t.Fatalf("suspicious clustering on 000000: %d", seen["000000"])
	}
}

func TestHashApprovalCode_StableHex(t *testing.T) {
	h1 := HashApprovalCode("123456")
	h2 := HashApprovalCode("123456")
	if h1 != h2 {
		t.Fatal("hash not stable")
	}
	if len(h1) != 64 {
		t.Fatalf("hash hex len %d", len(h1))
	}
	if HashApprovalCode("123456") == HashApprovalCode("123457") {
		t.Fatal("hash collides on adjacent codes")
	}
}

func TestMaskPhone_T35(t *testing.T) {
	cases := map[string]string{
		"+13102957704": "+1***7704",
		"+447911123456": "+44***3456",
		"":              "",
		"shortish":      "shortish",
	}
	for in, want := range cases {
		got := MaskPhone(in)
		if got != want {
			t.Errorf("MaskPhone(%q) = %q want %q", in, got, want)
		}
	}
}
