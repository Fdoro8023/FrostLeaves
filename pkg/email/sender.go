package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is one outbound email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a Message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPSender sends mail directly over the server's own internet connection.
// No mesh networking is involved: a plain TCP/TLS dial to the configured host is
// used. The dialer is injectable so tests never touch the network.
type SMTPSender struct {
	cfg  Config
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
	now  func() time.Time
}

// NewSMTPSender returns a sender for the given configuration.
func NewSMTPSender(cfg Config) *SMTPSender {
	d := &net.Dialer{Timeout: 15 * time.Second}
	return &SMTPSender{cfg: cfg, dial: d.DialContext, now: time.Now}
}

// BuildPayload renders the RFC 5322 message (exported for testability).
func (s *SMTPSender) BuildPayload(msg Message) []byte {
	headers := []string{
		"From: " + FormatFrom(s.cfg.FromName, s.cfg.FromEmail),
		"To: " + msg.To,
		"Subject: " + msg.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Date: " + s.now().Format(time.RFC1123Z),
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + msg.Body + "\r\n")
}

// Send delivers one message.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := s.cfg.Validate(); err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	client, err := s.dialClient(ctx, addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email: auth: %w", err)
			}
		}
	}
	if err := client.Mail(s.cfg.FromEmail); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := w.Write(s.BuildPayload(msg)); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return client.Quit()
}

func (s *SMTPSender) dialClient(ctx context.Context, addr string) (*smtp.Client, error) {
	switch s.cfg.Security {
	case "ssl":
		conn, err := s.dial(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("email: dial %s: %w", addr, err)
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: s.cfg.Host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("email: tls handshake: %w", err)
		}
		c, err := smtp.NewClient(tlsConn, s.cfg.Host)
		if err != nil {
			tlsConn.Close()
			return nil, err
		}
		return c, nil
	case "starttls", "none":
		conn, err := s.dial(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("email: dial %s: %w", addr, err)
		}
		c, err := smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if s.cfg.Security == "starttls" {
			if ok, _ := c.Extension("STARTTLS"); !ok {
				c.Close()
				return nil, fmt.Errorf("email: server does not support STARTTLS")
			}
			if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
				c.Close()
				return nil, fmt.Errorf("email: STARTTLS: %w", err)
			}
		}
		return c, nil
	default:
		return nil, fmt.Errorf("email: unknown security mode %q", s.cfg.Security)
	}
}
