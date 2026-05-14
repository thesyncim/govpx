package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPairingBrokerIssuesShortLivedOneTimeTokens(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newPairingBroker(time.Minute)

	token, expires, err := b.current(now)
	if err != nil {
		t.Fatalf("current returned error: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if expires != now.Add(time.Minute) {
		t.Fatalf("expires = %v, want %v", expires, now.Add(time.Minute))
	}
	if !b.valid(token, now.Add(30*time.Second)) {
		t.Fatal("token should be valid before expiry")
	}
	if !b.consume(token, now.Add(30*time.Second)) {
		t.Fatal("first consume failed")
	}
	if b.consume(token, now.Add(31*time.Second)) {
		t.Fatal("second consume succeeded for one-time token")
	}

	next, _, err := b.current(now.Add(32 * time.Second))
	if err != nil {
		t.Fatalf("current after consume returned error: %v", err)
	}
	if next == token {
		t.Fatal("current reused a consumed token")
	}
}

func TestPairingBrokerExpiresTokens(t *testing.T) {
	now := time.Unix(2000, 0)
	b := newPairingBroker(time.Minute)

	token, _, err := b.current(now)
	if err != nil {
		t.Fatalf("current returned error: %v", err)
	}
	if b.valid(token, now.Add(time.Minute)) {
		t.Fatal("token valid at expiry boundary")
	}

	next, _, err := b.current(now.Add(time.Minute))
	if err != nil {
		t.Fatalf("current after expiry returned error: %v", err)
	}
	if next == token {
		t.Fatal("current reused an expired token")
	}
}

func TestOfferEndpointAcceptsPairingURLs(t *testing.T) {
	got, err := offerEndpoint("https://remote.example/pair/abc-123")
	if err != nil {
		t.Fatalf("offerEndpoint returned error: %v", err)
	}
	if got != "https://remote.example/offer?token=abc-123" {
		t.Fatalf("offerEndpoint = %q", got)
	}
}

func TestOfferEndpointAcceptsExplicitTokenQuery(t *testing.T) {
	got, err := offerEndpoint("remote.example?token=abc-123")
	if err != nil {
		t.Fatalf("offerEndpoint returned error: %v", err)
	}
	if got != "http://remote.example/offer?token=abc-123" {
		t.Fatalf("offerEndpoint = %q", got)
	}
}

func TestNewPairingTokenIsURLSafe(t *testing.T) {
	token, err := newPairingToken()
	if err != nil {
		t.Fatalf("newPairingToken returned error: %v", err)
	}
	if token == "" || strings.ContainsAny(token, "/+=") {
		t.Fatalf("token = %q, want non-empty raw URL-safe base64", token)
	}
}

func TestPairingHandlersRenderDashboardAndQR(t *testing.T) {
	s := remoteHTTPServer{
		pairing:        newPairingBroker(time.Minute),
		publicBase:     "http://remote.example",
		requirePairing: true,
	}

	index := httptest.NewRecorder()
	s.handleIndex(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d", index.Code)
	}
	if body := index.Body.String(); !strings.Contains(body, "http://remote.example/pair/") {
		t.Fatalf("index body missing pairing URL:\n%s", body)
	}

	qr := httptest.NewRecorder()
	s.handleQR(qr, httptest.NewRequest(http.MethodGet, "/qr", nil))
	if qr.Code != http.StatusOK {
		t.Fatalf("qr status = %d", qr.Code)
	}
	if got := qr.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("qr content-type = %q", got)
	}
	if png := qr.Body.Bytes(); len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("qr response is not a PNG")
	}
}
