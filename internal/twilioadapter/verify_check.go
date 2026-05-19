package twilioadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// VerifyCheckResult captures the minimal Twilio Verify API VerificationCheck
// response. Approved is true iff Status == "approved".
type VerifyCheckResult struct {
	Status      string `json:"status"`
	SID         string `json:"sid"`
	To          string `json:"to"`
	ServiceSid  string `json:"service_sid"`
	DateCreated string `json:"date_created"`
	Approved    bool   `json:"-"`
}

// ErrNoPendingVerification is returned when Twilio responds 404 to a
// VerificationCheck — i.e. no Verify session exists for this phone
// (expired or never created).
var ErrNoPendingVerification = errors.New("twilio: no pending verification for this phone")

// CheckVerification submits a code to Twilio's VerificationCheck endpoint
// for the configured Verify Service. Returns result.Approved=true when
// Twilio reports status="approved". Returns ErrNoPendingVerification on
// HTTP 404. Returns a wrapped error (with raw body for diagnostics) on any
// other non-2xx.
func (c *TwilioClient) CheckVerification(ctx context.Context, to, code string) (VerifyCheckResult, error) {
	var result VerifyCheckResult
	endpoint := fmt.Sprintf("%s/v2/Services/%s/VerificationCheck", c.baseURL, url.PathEscape(c.cfg.VerifyServiceSID))

	form := url.Values{}
	form.Set("To", to)
	form.Set("Code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return result, err
	}
	req.SetBasicAuth(c.cfg.AccountSID, c.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("twilio verify check request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return result, ErrNoPendingVerification
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("twilio verify check %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return result, fmt.Errorf("decode twilio verify check response: %w (body=%s)", err, string(respBody))
	}
	result.Approved = result.Status == "approved"
	return result, nil
}
