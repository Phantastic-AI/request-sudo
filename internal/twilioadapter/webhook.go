package twilioadapter

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
)

// ValidateTwilioSignature checks the X-Twilio-Signature header per
// Twilio's docs and ADR-0005 T1–T5:
//
//   - HMAC-SHA1 over (full URL + sorted form params concatenated as
//     key+value pairs), keyed by the Twilio Auth Token, base64 encoded.
//   - Constant-time comparison (T5).
//   - Empty signature → reject (T2).
//   - Wrong signature → reject (T3).
//
// fullURL must be the exact URL Twilio will sign (including scheme +
// host + path + query), matching the configured public URL prefix.
// Host-mismatch enforcement (T4) is the caller's responsibility — pass
// in the configured public URL, not the request's Host header, so a
// captured signature can't be replayed against a different host.
func ValidateTwilioSignature(authToken, fullURL string, form url.Values, signatureHeader string) bool {
	if signatureHeader == "" || authToken == "" {
		return false
	}
	expected := computeTwilioSignature(authToken, fullURL, form)
	// constant-time compare
	return hmac.Equal([]byte(expected), []byte(signatureHeader))
}

func computeTwilioSignature(authToken, fullURL string, form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		b.WriteString(k)
		// Twilio concatenates the first value per key (per their docs,
		// repeated keys are rare; production adapter would handle them
		// explicitly).
		b.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ParseReply takes a normalized SMS body (Twilio's `Body` form field)
// and returns the parsed approval decision. ADR-0005 T11.
//
// Accepted forms (case-insensitive, single-space tolerant):
//
//	"yes 123456"  → ReplyApprove, "123456"
//	"no 123456"   → ReplyDeny, "123456"
//	anything else → ReplyMalformed, ""
//
// T6 also requires the code to be exactly 6 ASCII digits; this is
// enforced here so callers don't have to repeat the check.
func ParseReply(body string) (ReplyDecision, string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return ReplyMalformed, ""
	}
	fields := strings.Fields(body)
	if len(fields) != 2 {
		return ReplyMalformed, ""
	}
	decision := strings.ToLower(fields[0])
	code := strings.TrimSpace(fields[1])
	if !isSixDigit(code) {
		return ReplyMalformed, ""
	}
	switch decision {
	case "yes", "y", "approve":
		return ReplyApprove, code
	case "no", "n", "deny":
		return ReplyDeny, code
	default:
		return ReplyMalformed, ""
	}
}

// ReplyDecision is the parsed verdict from an inbound SMS body.
type ReplyDecision int

const (
	ReplyMalformed ReplyDecision = iota
	ReplyApprove
	ReplyDeny
)

func isSixDigit(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
