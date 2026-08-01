package exeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDomainAddDNSFailureReturnsAppError(t *testing.T) {
	// exe.dev returns HTTP 200 with a JSON error body when DNS hasn't propagated.
	body, _ := json.Marshal(map[string]string{
		"domain":   "nesins-finance.codevm.xyz",
		"error":    "DNS for nesins-finance.codevm.xyz does not point to codevm.exe.xyz",
		"expected": "codevm.exe.xyz",
		"vm_name":  "codevm",
		"status":   "error",
	})
	failCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failCount < 2 {
			failCount++
			w.WriteHeader(200)
			w.Write(body)
			return
		}
		// third call: success
		w.WriteHeader(200)
		fmt.Fprint(w, `{"domain":"nesins-finance.codevm.xyz","status":"ok"}`)
	}))
	defer srv.Close()

	// temporarily point the client at the test server
	orig := execEndpoint
	setExecEndpoint(srv.URL)
	defer setExecEndpoint(orig)

	c := &Client{Token: "test", HTTP: srv.Client()}

	// First call: should return AppError (retryable)
	_, err := c.DomainAdd("codevm", "nesins-finance.codevm.xyz")
	if err == nil {
		t.Fatal("expected AppError for DNS failure, got nil")
	}
	var ae *AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if !Retryable(err) {
		t.Fatal("DNS AppError should be retryable")
	}
	if ae.Domain != "nesins-finance.codevm.xyz" {
		t.Errorf("domain = %q", ae.Domain)
	}

	// After 2 failures, third call succeeds
	for i := 0; i < 3; i++ {
		out, err := c.DomainAdd("codevm", "nesins-finance.codevm.xyz")
		if err == nil {
			t.Logf("succeeded on call %d: %s", i+1, out)
			return
		}
	}
	t.Fatal("should have succeeded by the third call")
}

func TestDomainAddAuthErrorNotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	orig := execEndpoint
	setExecEndpoint(srv.URL)
	defer setExecEndpoint(orig)

	c := &Client{Token: "bad", HTTP: srv.Client()}
	_, err := c.DomainAdd("codevm", "app.example.com")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if Retryable(err) {
		t.Fatal("401 should NOT be retryable")
	}
}
