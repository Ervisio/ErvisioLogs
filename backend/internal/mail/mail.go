// Package mail sends plain-text email through an SMTP server.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Config is the SMTP server and sender. Security is starttls, tls (implicit TLS, port 465) or none.
type Config struct {
	Host     string
	Port     int
	Security string
	Username string
	Password string
	From     string
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, c Config, m Message) error
}

// sendTimeout bounds a whole delivery: a stuck server must not hold a request or a goroutine.
const sendTimeout = 30 * time.Second

// SMTP delivers each message over a new connection.
type SMTP struct{}

func (SMTP) Send(ctx context.Context, c Config, m Message) error {
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return fmt.Errorf("sender address: %w", err)
	}
	msg, err := Build(from, m, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	conn, err := dial(ctx, c)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; set security to tls or none")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if c.Username != "" {
		// PlainAuth refuses to send the password over an unencrypted connection to another host.
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return err
	}
	if err := cl.Rcpt(m.To); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

func dial(ctx context.Context, c Config) (net.Conn, error) {
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	if c.Security == "tls" {
		d := tls.Dialer{Config: &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}}
		return d.DialContext(ctx, "tcp", addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// Build renders m as an RFC 5322 message with a quoted-printable UTF-8 body.
func Build(from *mail.Address, m Message, now time.Time) ([]byte, error) {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return nil, fmt.Errorf("recipient address: %w", err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	var b bytes.Buffer
	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// oneLine keeps a header value on one line, so text from users cannot add headers.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' }), " ")
}
