package twilioadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCheckVerification_Approved(t *testing.T) {
	var capturedPath string
	var capturedForm url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		capturedForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"approved","sid":"VEx","to":"+14155550123","service_sid":"VAtestservice","date_created":"2026-05-19T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewTwilioClient(TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}).WithBaseURL(srv.URL)

	result, err := client.CheckVerification(context.Background(), "+14155550123", "123456")
	if err != nil {
		t.Fatalf("CheckVerification: %v", err)
	}
	if !result.Approved {
		t.Errorf("Approved=false, want true")
	}
	if result.Status != "approved" {
		t.Errorf("Status=%q, want approved", result.Status)
	}
	if result.SID != "VEx" {
		t.Errorf("SID=%q, want VEx", result.SID)
	}

	// Sanity: assert endpoint + form
	if !strings.Contains(capturedPath, "/v2/Services/") || !strings.HasSuffix(capturedPath, "/VerificationCheck") {
		t.Errorf("path=%q, want /v2/Services/.../VerificationCheck", capturedPath)
	}
	if got := capturedForm.Get("To"); got != "+14155550123" {
		t.Errorf("To=%q, want +14155550123", got)
	}
	if got := capturedForm.Get("Code"); got != "123456" {
		t.Errorf("Code=%q, want 123456", got)
	}
}

func TestCheckVerification_Pending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pending","sid":"VEy","to":"+14155550123","service_sid":"VAtestservice","date_created":"2026-05-19T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewTwilioClient(TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}).WithBaseURL(srv.URL)

	result, err := client.CheckVerification(context.Background(), "+14155550123", "000000")
	if err != nil {
		t.Fatalf("CheckVerification: %v", err)
	}
	if result.Approved {
		t.Errorf("Approved=true, want false on pending")
	}
	if result.Status != "pending" {
		t.Errorf("Status=%q, want pending", result.Status)
	}
}

func TestCheckVerification_NotFoundReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":20404,"message":"not found"}`))
	}))
	defer srv.Close()

	client := NewTwilioClient(TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}).WithBaseURL(srv.URL)

	_, err := client.CheckVerification(context.Background(), "+14155550123", "123456")
	if !errors.Is(err, ErrNoPendingVerification) {
		t.Fatalf("expected ErrNoPendingVerification, got %v", err)
	}
}

func TestCheckVerification_OtherErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":20500,"message":"server explosion"}`))
	}))
	defer srv.Close()

	client := NewTwilioClient(TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "tokentest",
		VerifyServiceSID: "VAtestservice",
	}).WithBaseURL(srv.URL)

	_, err := client.CheckVerification(context.Background(), "+14155550123", "123456")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "server explosion") {
		t.Errorf("error should include status and body, got: %v", err)
	}
}
