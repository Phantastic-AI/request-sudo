package twilioadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TwilioClient sends approval-prompt SMS via Twilio Verify API.
// ADR-0005a (2026-05-19) reversed the original ADR-0005 decision to use the
// Messages API: empirical testing showed Messages API to US numbers from
// unregistered 10DLC senders fails with error 30034 (silently dropped by
// carriers), while Verify routes through Twilio's pre-registered managed
// sender pool and bypasses 10DLC entirely. Adapter does NOT pass CustomCode
// (paid feature) or CustomFriendlyName (rejected with 60204 by some Verify
// Services including moltpod prod). Twilio owns the code lifecycle; binding
// to request_id is preserved by the adapter's same-phone serialization
// invariant (see ADR-0005a T34 and pending_set.go).
type TwilioClient struct {
	cfg        TwilioConfig
	httpClient *http.Client
	baseURL    string // overrideable for tests
}

func NewTwilioClient(cfg TwilioConfig) *TwilioClient {
	return &TwilioClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    "https://verify.twilio.com",
	}
}

// WithBaseURL is for test overrides (httptest.Server).
func (c *TwilioClient) WithBaseURL(u string) *TwilioClient {
	c.baseURL = u
	return c
}

// VerifySendResult captures the minimal Twilio Verify API Verifications response.
type VerifySendResult struct {
	SID         string `json:"sid"`
	Status      string `json:"status"`
	To          string `json:"to"`
	ServiceSid  string `json:"service_sid"`
	DateCreated string `json:"date_created"`
}

// SendVerificationSMS sends an approval-prompt SMS via the Twilio Verify
// API. Twilio owns the verification-code lifecycle (generation, expiry,
// single-use, attempt counter) under ADR-0005a. Returns the parsed
// Verifications result on success; returns an error (with the raw response
// body for diagnostics) on any non-2xx. Per ADR-0005 T25/T27, the adapter
// NEVER auto-retries — caller decides.
func (c *TwilioClient) SendVerificationSMS(ctx context.Context, to string) (VerifySendResult, error) {
	var result VerifySendResult
	endpoint := fmt.Sprintf("%s/v2/Services/%s/Verifications", c.baseURL, url.PathEscape(c.cfg.VerifyServiceSID))

	form := url.Values{}
	form.Set("To", to)
	form.Set("Channel", "sms")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return result, err
	}
	req.SetBasicAuth(c.cfg.AccountSID, c.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("twilio verify request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("twilio verify %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return result, fmt.Errorf("decode twilio verify response: %w (body=%s)", err, string(respBody))
	}
	return result, nil
}
