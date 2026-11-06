package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTelegramUsesFixedDestinationAndNoMarkup(t *testing.T) {
	cl := &Client{http: &http.Client{Transport: telegramTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.Method != "POST" {
			t.Fatalf("unexpected destination: %s", r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["chat_id"] != "-12345" || body["parse_mode"] != nil {
			t.Fatalf("unsafe chat rendering: %v", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}}
	if err := cl.SendTelegram(context.Background(), "bot-secret", "-12345", Payload{Type: "alert.firing", RuleName: "<b>unsafe markup</b>"}); err != nil {
		t.Fatal(err)
	}
}
func TestTelegramErrorsNeverExposeBotToken(t *testing.T) {
	secret := "12345:sensitive-token"
	for _, networkFailure := range []bool{true, false} {
		cl := &Client{http: &http.Client{Transport: telegramTransport(func(r *http.Request) (*http.Response, error) {
			if networkFailure {
				return nil, errors.New("request failed at " + r.URL.String())
			}
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"` + secret + `"}`)), Header: make(http.Header)}, nil
		})}}
		err := cl.SendTelegram(context.Background(), secret, "123", Payload{})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}
