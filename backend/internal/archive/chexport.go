package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CHHTTP runs queries over the ClickHouse HTTP interface, which can stream a result in an output
// format (Parquet) that the native protocol cannot. Credentials travel in headers, never in the URL,
// so they stay out of access logs.
type CHHTTP struct {
	base     string
	database string
	user     string
	password string
	client   *http.Client
}

func NewCHHTTP(baseURL, database, user, password string) *CHHTTP {
	return &CHHTTP{base: strings.TrimRight(baseURL, "/"), database: database, user: user, password: password,
		client: &http.Client{Timeout: 30 * time.Minute}}
}

// Query runs query with typed parameters ({name:Type} placeholders) and streams the response
// body to w. settings are ClickHouse settings for this query only.
func (c *CHHTTP) Query(ctx context.Context, query string, params, settings map[string]string, w io.Writer) error {
	q := url.Values{}
	q.Set("database", c.database)
	// Buffer the whole result on the server first, so an error in the middle of the query comes
	// back as an HTTP error instead of a 200 with a truncated body.
	q.Set("wait_end_of_query", "1")
	for k, v := range params {
		q.Set("param_"+k, v)
	}
	for k, v := range settings {
		q.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/?"+q.Encode(), strings.NewReader(query))
	if err != nil {
		return err
	}
	req.Header.Set("X-ClickHouse-User", c.user)
	req.Header.Set("X-ClickHouse-Key", c.password)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse http: %w", redactURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("clickhouse http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	// A server-side exception after the headers were sent shows up here.
	if ex := resp.Header.Get("X-ClickHouse-Exception-Code"); ex != "" {
		return fmt.Errorf("clickhouse http exception %s", ex)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// redactURL drops the query string from a url.Error: it carries the query parameters.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		u := ue.URL
		if i := strings.IndexByte(u, '?'); i >= 0 {
			u = u[:i]
		}
		return &url.Error{Op: ue.Op, URL: u, Err: ue.Err}
	}
	return err
}
