// Package notify delivers alert notifications to webhook channels (ADR 0010).
//
// Every delivery is a POST with a JSON body. For the "ervisio" format the body is signed:
//
//	X-Ervisio-Timestamp: <unix seconds>
//	X-Ervisio-Signature: v1=<hex HMAC-SHA256(key, timestamp + "." + body)>
//	X-Ervisio-Event-Id:  <uuid, stable across retries>
//
// The key is derived from the server secret, the channel id and its secret version, so it
// is never stored. Slack and Discord formats send their incoming-webhook body shape and rely
// on the secret in their URL.
//
// The client refuses to connect to loopback, private, link-local, CGNAT, multicast and
// unspecified addresses unless AllowPrivate is set, checks the address actually dialled (not
// only the name), and does not follow redirects.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Formats accepted for a channel.
const (
	FormatErvisio = "ervisio"
	FormatSlack   = "slack"
	FormatDiscord = "discord"
)

// ValidFormat reports whether f is a known channel format.
func ValidFormat(f string) bool {
	return f == FormatErvisio || f == FormatSlack || f == FormatDiscord
}

// DeriveKey returns the signing key of a channel at a secret version.
func DeriveKey(serverSecret []byte, channelID uuid.UUID, version int32) []byte {
	m := hmac.New(sha256.New, serverSecret)
	fmt.Fprintf(m, "webhook:%s:%d", channelID, version)
	return m.Sum(nil)
}

// DisplaySecret is the form shown to the user once, so receivers can verify signatures.
// Receivers use the decoded bytes after the prefix as the HMAC key.
func DisplaySecret(key []byte) string {
	return "whsec_" + base64.RawURLEncoding.EncodeToString(key)
}

// Sign returns the v1 signature header value.
func Sign(key []byte, ts int64, body []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(strconv.FormatInt(ts, 10)))
	m.Write([]byte("."))
	m.Write(body)
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header against the body, for receivers and tests. tolerance
// bounds the clock difference; 0 disables the check.
func Verify(key []byte, tsHeader, sigHeader string, body []byte, now time.Time, tolerance time.Duration) bool {
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return false
	}
	if tolerance > 0 {
		d := now.Sub(time.Unix(ts, 0))
		if d < -tolerance || d > tolerance {
			return false
		}
	}
	return hmac.Equal([]byte(Sign(key, ts, body)), []byte(sigHeader))
}

// ValidateURL checks a channel URL before it is stored. Public targets need https;
// allowPrivate also accepts http and private hosts (local receivers, LAN).
func ValidateURL(raw string, allowPrivate bool) error {
	if len(raw) < 8 || len(raw) > 2048 {
		return errors.New("url must be 8 to 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("url is not an absolute URL")
	}
	if u.User != nil {
		return errors.New("url must not carry user credentials")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowPrivate {
			return errors.New("url must use https")
		}
	default:
		return errors.New("url must use https")
	}
	if allowPrivate {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if addr, err := netip.ParseAddr(host); err == nil && !Public(addr) {
		return errors.New("url points at a private or reserved address")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("url points at a private or reserved address")
	}
	return nil
}

// RedactURL keeps scheme and host and hides path and query, which may hold a token.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(invalid)"
	}
	s := u.Scheme + "://" + u.Host
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		s += "/..."
	}
	return s
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Public reports whether addr is a globally routable unicast address.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(), addr.IsUnspecified(), addr.IsLoopback(), addr.IsPrivate(),
		addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast():
		return false
	case addr.Is4() && cgnat.Contains(addr):
		return false
	case addr.Is4() && addr.As4()[0] == 0: // 0.0.0.0/8
		return false
	case addr.Is4() && addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
		return false
	}
	return true
}

var errBlocked = errors.New("destination address is private or reserved")

// Client sends webhook deliveries.
type Client struct {
	http     *http.Client
	attempts int
	backoff  time.Duration
}

// NewClient builds a client. allowPrivate is asked on every dial; true lifts the address filter.
func NewClient(allowPrivate func() bool) *Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	// Control sees the resolved address being dialled, so DNS rebinding cannot slip a private
	// address past a check made on the name.
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		if allowPrivate() {
			return nil
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("unexpected address %q", address)
		}
		if !Public(ap.Addr()) {
			return errBlocked
		}
		return nil
	}
	tr := &http.Transport{
		Proxy:                 nil, // a proxy would hide the dialled address from the filter
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
	}
	return &Client{
		http: &http.Client{
			Transport: tr,
			Timeout:   10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		attempts: 3,
		backoff:  time.Second,
	}
}

// Delivery is one notification to one channel.
type Delivery struct {
	URL     string
	Format  string
	Key     []byte // signs the ervisio format
	Payload Payload
}

// Payload is the alert document sent to channels.
type Payload struct {
	Type        string     `json:"type"` // alert.firing, alert.resolved, alert.error, test
	EventID     uuid.UUID  `json:"event_id"`
	SentAt      time.Time  `json:"sent_at"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	RuleID      *uuid.UUID `json:"rule_id,omitempty"`
	RuleName    string     `json:"rule_name,omitempty"`
	Value       *int64     `json:"value,omitempty"`
	Threshold   *int64     `json:"threshold,omitempty"`
	WindowSec   int32      `json:"window_seconds,omitempty"`
	Detail      string     `json:"detail,omitempty"`
}

// Summary is the text used by chat formats.
func (p Payload) Summary() string {
	var b strings.Builder
	switch p.Type {
	case "alert.firing":
		b.WriteString("[FIRING] ")
	case "alert.resolved":
		b.WriteString("[RESOLVED] ")
	case "alert.error":
		b.WriteString("[ERROR] ")
	default:
		return "Ervisio Logs test notification"
	}
	b.WriteString(p.RuleName)
	if p.Value != nil && p.Threshold != nil {
		fmt.Fprintf(&b, ": %d events in %s (threshold %d)", *p.Value, time.Duration(p.WindowSec)*time.Second, *p.Threshold)
	}
	if p.Detail != "" {
		b.WriteString(". ")
		b.WriteString(p.Detail)
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Body encodes the request body for a format.
func Body(format string, p Payload) ([]byte, error) {
	switch format {
	case FormatSlack:
		return json.Marshal(map[string]string{"text": clip(p.Summary(), 3000)})
	case FormatDiscord:
		// allowed_mentions with an empty parse list stops @everyone in a rule name pinging.
		return json.Marshal(map[string]any{
			"content":          clip(p.Summary(), 1900),
			"allowed_mentions": map[string]any{"parse": []string{}},
		})
	default:
		return json.Marshal(p)
	}
}

// Send delivers d, retrying network errors, 429 and 5xx. The returned error is safe to
// store and show: it never contains the URL.
func (c *Client) Send(ctx context.Context, d Delivery) error {
	body, err := Body(d.Format, d.Payload)
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < c.attempts; attempt++ {
		if attempt > 0 {
			t := time.NewTimer(c.backoff * time.Duration(1<<(attempt-1)))
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		retry, err := c.once(ctx, d, body)
		if err == nil {
			return nil
		}
		last = err
		if !retry {
			break
		}
	}
	return last
}

func (c *Client) once(ctx context.Context, d Delivery, body []byte) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return false, errors.New("invalid channel url")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ErvisioLogs-Webhook/1")
	req.Header.Set("X-Ervisio-Event-Id", d.Payload.EventID.String())
	if d.Format == FormatErvisio || d.Format == "" {
		ts := time.Now().Unix()
		req.Header.Set("X-Ervisio-Timestamp", strconv.FormatInt(ts, 10))
		req.Header.Set("X-Ervisio-Signature", Sign(d.Key, ts, body))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, errBlocked) {
			return false, errBlocked
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, errors.New("could not reach the destination")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, fmt.Errorf("destination answered %d (redirects are not followed)", resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("destination answered %d", resp.StatusCode)
	default:
		return false, fmt.Errorf("destination answered %d", resp.StatusCode)
	}
}
