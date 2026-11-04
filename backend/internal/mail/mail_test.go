package mail

import (
	"bufio"
	"context"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildKeepsHeadersOnOneLine(t *testing.T) {
	from := &mail.Address{Name: "Ervisio Logs", Address: "logs@example.com"}
	msg, err := Build(from, Message{To: "ada@example.com", Subject: "Join Café\r\nBcc: evil@example.com", Body: "Hello\nà presto"}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	head, body, _ := strings.Cut(string(msg), "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("subject injected a header:\n%s", head)
	}
	if !strings.Contains(head, "Subject: =?utf-8?q?") || !strings.Contains(head, "Message-ID: <") {
		t.Fatalf("headers:\n%s", head)
	}
	if body != "Hello\r\n=C3=A0 presto" {
		t.Fatalf("body %q", body)
	}
	if _, err := Build(from, Message{To: "not an address"}, time.Now()); err == nil {
		t.Fatal("bad recipient accepted")
	}
}

func TestSendPlain(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go fakeServer(ln, got)

	c := Config{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Security: "none", From: "Ervisio Logs <logs@example.com>"}
	if err := (SMTP{}).Send(context.Background(), c, Message{To: "ada@example.com", Subject: "Hi", Body: "Body"}); err != nil {
		t.Fatal(err)
	}
	data := <-got
	if !strings.Contains(data, "MAIL FROM:<logs@example.com>") || !strings.Contains(data, "RCPT TO:<ada@example.com>") || !strings.Contains(data, "\r\nBody") {
		t.Fatalf("session:\n%s", data)
	}

	c.Security = "starttls"
	if err := (SMTP{}).Send(context.Background(), c, Message{To: "ada@example.com"}); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS error, got %v", err)
	}
}

// fakeServer answers one SMTP session per connection, without extensions, and reports the first.
func fakeServer(ln net.Listener, got chan<- string) {
	first := true
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		var log strings.Builder
		rd := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		reply("220 fake")
		inData := false
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				break
			}
			log.WriteString(line)
			switch {
			case inData:
				if line == ".\r\n" {
					inData = false
					reply("250 queued")
				}
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				reply("250 fake")
			case strings.HasPrefix(line, "DATA"):
				inData = true
				reply("354 go")
			case strings.HasPrefix(line, "QUIT"):
				reply("221 bye")
			default:
				reply("250 ok")
			}
		}
		conn.Close()
		if first {
			got <- log.String()
			first = false
		}
	}
}
