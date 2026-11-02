package api

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		trust  bool
		xff    []string
		remote string
		want   string
	}{
		{"proxy not trusted", false, []string{"203.0.113.9"}, "10.0.0.1:1234", "10.0.0.1"},
		{"no header", true, nil, "10.0.0.1:1234", "10.0.0.1"},
		{"single entry", true, []string{"203.0.113.9"}, "10.0.0.1:1234", "203.0.113.9"},
		{"last entry of a line", true, []string{"6.6.6.6, 203.0.113.9"}, "10.0.0.1:1234", "203.0.113.9"},
		{"last of several lines", true, []string{"6.6.6.6", "7.7.7.7, 203.0.113.9"}, "10.0.0.1:1234", "203.0.113.9"},
		{"garbage after the last valid", true, []string{"203.0.113.9, not-an-ip", ""}, "10.0.0.1:1234", "203.0.113.9"},
		{"IPv4-mapped", true, []string{"::ffff:203.0.113.9"}, "10.0.0.1:1234", "203.0.113.9"},
		{"IPv6", true, []string{" 2001:db8::1 "}, "10.0.0.1:1234", "2001:db8::1"},
		{"nothing valid", true, []string{"junk, 300.1.1.1"}, "10.0.0.1:1234", "10.0.0.1"},
		{"mapped remote", false, nil, "[::ffff:10.0.0.1]:1234", "10.0.0.1"},
		{"remote without port", false, nil, "10.0.0.1", "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{RemoteAddr: tc.remote, Header: http.Header{}}
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := (&API{TrustProxy: tc.trust}).clientIP(r); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateIPAggregatesIPv6(t *testing.T) {
	a := &API{TrustProxy: true}
	key := func(ip string) string {
		r := &http.Request{RemoteAddr: "10.0.0.1:1", Header: http.Header{"X-Forwarded-For": {ip}}}
		return a.rateIP(r)
	}
	if key("2001:db8:1:2::1") != key("2001:db8:1:2:ffff::9") {
		t.Fatal("same /64 must share a key")
	}
	if key("2001:db8:1:2::1") == key("2001:db8:1:3::1") {
		t.Fatal("different /64 must not share a key")
	}
}
