package twilioadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSendVerificationSMS_PostsToVerifyEndpoint(t *testing.T) {
	var capturedPath string
	var capturedForm url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		capturedForm = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sid":          "VEabc123",
			"status":       "pending",
			"to":           "+14155550123",
			"service_sid":  "VAtestservice",
			"date_created": "2026-05-19T00:00:00Z",
		})
	}))
	defer srv.Close()

	cfg := TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}
	client := NewTwilioClient(cfg).WithBaseURL(srv.URL)

	result, err := client.SendVerificationSMS(context.Background(), "+14155550123")
	if err != nil {
		t.Fatalf("SendVerificationSMS: %v", err)
	}

	// Assertion 1: URL contains /v2/Services/ and ends in /Verifications
	if !strings.Contains(capturedPath, "/v2/Services/") {
		t.Errorf("path missing /v2/Services/: %q", capturedPath)
	}
	if !strings.HasSuffix(capturedPath, "/Verifications") {
		t.Errorf("path does not end in /Verifications: %q", capturedPath)
	}
	if !strings.Contains(capturedPath, "VAtestservice") {
		t.Errorf("path does not include VerifyServiceSID: %q", capturedPath)
	}

	// Assertion 2: form has To + Channel=sms, NO CustomCode/CustomFriendlyName/Body
	if got := capturedForm.Get("To"); got != "+14155550123" {
		t.Errorf("To=%q, want +14155550123", got)
	}
	if got := capturedForm.Get("Channel"); got != "sms" {
		t.Errorf("Channel=%q, want sms", got)
	}
	for _, forbidden := range []string{"CustomCode", "CustomFriendlyName", "Body", "From"} {
		if _, present := capturedForm[forbidden]; present {
			t.Errorf("form must NOT contain %s, got %q", forbidden, capturedForm.Get(forbidden))
		}
	}

	// Assertion 3: response parsing
	if result.SID != "VEabc123" {
		t.Errorf("SID=%q, want VEabc123", result.SID)
	}
	if result.Status != "pending" {
		t.Errorf("Status=%q, want pending", result.Status)
	}
	if result.To != "+14155550123" {
		t.Errorf("To=%q, want +14155550123", result.To)
	}
	if result.ServiceSid != "VAtestservice" {
		t.Errorf("ServiceSid=%q, want VAtestservice", result.ServiceSid)
	}
}

func TestSendVerificationSMS_NonSuccessReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":60200,"message":"Invalid parameter"}`))
	}))
	defer srv.Close()

	client := NewTwilioClient(TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}).WithBaseURL(srv.URL)

	_, err := client.SendVerificationSMS(context.Background(), "+14155550123")
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid parameter") {
		t.Errorf("error should include raw body: %v", err)
	}
}

func TestNewTwilioClient_DefaultBaseURL(t *testing.T) {
	c := NewTwilioClient(TwilioConfig{})
	if c.baseURL != "https://verify.twilio.com" {
		t.Errorf("default baseURL=%q, want https://verify.twilio.com", c.baseURL)
	}
}
