package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/reinkrul/meeting-planner/internal/config"
)

// SMTPNotifier sends notification emails over SMTP. STARTTLS supported.
type SMTPNotifier struct {
	cfg      config.SMTPConfig
	password string
}

func NewSMTP(cfg config.SMTPConfig) (*SMTPNotifier, error) {
	pw := os.Getenv(cfg.PasswordEnv)
	if pw == "" {
		return nil, fmt.Errorf("env var %s is empty", cfg.PasswordEnv)
	}
	return &SMTPNotifier{cfg: cfg, password: pw}, nil
}

func (n *SMTPNotifier) Notify(ctx context.Context, subject, body string) error {
	msg := buildMessage(n.cfg.From, n.cfg.To, subject, body)
	addr := fmt.Sprintf("%s:%d", n.cfg.Host, n.cfg.Port)
	auth := smtp.PlainAuth("", n.cfg.Username, n.password, n.cfg.Host)

	// Honour ctx via a deadline-aware dialer.
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if n.cfg.StartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: n.cfg.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := c.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, to := range n.cfg.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return c.Quit()
}

// buildMessage produces a minimal RFC 5322 message.
func buildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
