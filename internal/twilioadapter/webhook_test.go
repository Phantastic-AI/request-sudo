package twilioadapter

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// T1–T5: webhook signature validation.
// T11 (partial): reply parsing.

func referenceSignature(token, fullURL string, form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestValidateTwilioSignature_T1_ValidSignatureAccepted(t *testing.T) {
	const token = "test-token"
	const fullURL = "https://example.com/twilio/inbound"
	form := url.Values{}
	form.Set("From", "+15551234")
	form.Set("Body", "yes 123456")
	form.Set("MessageSid", "SM999")
	sig := referenceSignature(token, fullURL, form)
	if !ValidateTwilioSignature(token, fullURL, form, sig) {
		t.Fatal("valid signature was rejected")
	}
}

func TestValidateTwilioSignature_T2_MissingHeaderRejected(t *testing.T) {
	form := url.Values{}
	form.Set("From", "+15551234")
	if ValidateTwilioSignature("token", "https://example.com/x", form, "") {
		t.Fatal("missing signature accepted")
	}
}

func TestValidateTwilioSignature_T3_WrongSignatureRejected(t *testing.T) {
	const fullURL = "https://example.com/twilio/inbound"
	form := url.Values{}
	form.Set("Body", "yes 123456")
	sigRight := referenceSignature("right-token", fullURL, form)
	sigWrongToken := referenceSignature("WRONG-TOKEN", fullURL, form)
	if ValidateTwilioSignature("right-token", fullURL, form, sigWrongToken) {
		t.Fatal("wrong-token signature accepted")
	}
	// tamper the body but keep the original signature
	formTamper := url.Values{}
	formTamper.Set("Body", "yes 999999")
	if ValidateTwilioSignature("right-token", fullURL, formTamper, sigRight) {
		t.Fatal("tampered-body signature accepted")
	}
}

func TestValidateTwilioSignature_T4_HostMismatchRejected(t *testing.T) {
	// T4 is enforced by passing the *configured* full URL, not the
	// request's Host. We simulate by signing for one URL and validating
	// against another.
	form := url.Values{}
	form.Set("Body", "yes 123456")
	sig := referenceSignature("token", "https://attacker.example/x", form)
	if ValidateTwilioSignature("token", "https://legit.example/x", form, sig) {
		t.Fatal("host-mismatch signature accepted")
	}
}

func TestValidateTwilioSignature_T5_ConstantTime(t *testing.T) {
	// hmac.Equal is constant-time by contract. This test simply asserts
	// we use it (covered by reading webhook.go), and that comparing two
	// different-length strings doesn't panic.
	form := url.Values{}
	form.Set("Body", "yes 123456")
	if ValidateTwilioSignature("token", "https://x.example/", form, "short") {
		t.Fatal("short sig accepted")
	}
}

func TestParseReply(t *testing.T) {
	cases := []struct {
		in       string
		decision ReplyDecision
		code     string
	}{
		{"yes 123456", ReplyApprove, "123456"},
		{"YES 123456", ReplyApprove, "123456"},
		{"Yes 123456", ReplyApprove, "123456"},
		{"y 123456", ReplyApprove, "123456"},
		{"approve 123456", ReplyApprove, "123456"},
		{"no 123456", ReplyDeny, "123456"},
		{"NO 123456", ReplyDeny, "123456"},
		{"n 123456", ReplyDeny, "123456"},
		{"deny 123456", ReplyDeny, "123456"},
		{"yes 12345", ReplyMalformed, ""},   // 5 digits
		{"yes 1234567", ReplyMalformed, ""}, // 7 digits
		{"yes abcdef", ReplyMalformed, ""},
		{"maybe 123456", ReplyMalformed, ""},
		{"123456", ReplyMalformed, ""},
		{"yes", ReplyMalformed, ""},
		{"", ReplyMalformed, ""},
	}
	for _, tc := range cases {
		gotDecision, gotCode := ParseReply(tc.in)
		if gotDecision != tc.decision || gotCode != tc.code {
			t.Errorf("ParseReply(%q) = (%d, %q) want (%d, %q)", tc.in, gotDecision, gotCode, tc.decision, tc.code)
		}
	}
}
