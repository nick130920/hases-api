package mailer

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/hases/hases-api/internal/config"
)

// Mailer encapsula envío SMTP. Si no hay host configurado,
// es no-op (operación segura en dev).
//
// Soporta dos modos de transporte:
//   - Implicit TLS (SMTPS) cuando el puerto es 465: abre tls.Dial directo.
//   - STARTTLS / texto plano cuando el puerto es 587 o 25: usa smtp.SendMail
//     con upgrade STARTTLS automático.
type Mailer struct {
	cfg config.Config
}

func New(cfg config.Config) *Mailer {
	return &Mailer{cfg: cfg}
}

func (m *Mailer) Enabled() bool {
	return strings.TrimSpace(m.cfg.SMTPHost) != "" && strings.TrimSpace(m.cfg.SMTPFrom) != ""
}

// envelopeFrom returns just the email address from SMTP_FROM, supporting
// both plain "user@host" and display-name forms like "Name <user@host>".
// SMTP envelopes (MAIL FROM) require a bare address, while the From header
// can carry the full display name.
func (m *Mailer) envelopeFrom() string {
	addr, err := mail.ParseAddress(m.cfg.SMTPFrom)
	if err != nil {
		return strings.TrimSpace(m.cfg.SMTPFrom)
	}
	return addr.Address
}

// Send envía un email simple con cuerpo en texto plano.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		return nil
	}
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.cfg.SMTPFrom, to, subject, body,
	))
	return m.deliver(to, msg)
}

// SendWithAttachment envía un email multipart/mixed con un único adjunto.
// Útil para enviar PDFs (ej. examen ocupacional a la IPS).
func (m *Mailer) SendWithAttachment(to, subject, body, filename, mimeType string, data []byte) error {
	if !m.Enabled() {
		return nil
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	boundary := "hases-mixed-boundary"

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", m.cfg.SMTPFrom)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary)

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", mimeType, filename)
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%q\r\n\r\n", filename)
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	return m.deliver(to, buf.Bytes())
}

// deliver routes the message either through implicit TLS (port 465) or
// through smtp.SendMail (which negotiates STARTTLS when available).
func (m *Mailer) deliver(to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)
	auth := smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)
	from := m.envelopeFrom()

	if m.cfg.SMTPPort == 465 {
		return m.sendImplicitTLS(addr, auth, from, []string{to}, msg)
	}
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// sendImplicitTLS opens a direct TLS connection (SMTPS) and runs the SMTP
// dialogue manually. Required for relays that only listen on port 465 or
// when the network blocks STARTTLS submission ports.
func (m *Mailer) sendImplicitTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: m.cfg.SMTPHost})
	if err != nil {
		return fmt.Errorf("smtps dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, m.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Quit()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("rcpt to: %w", err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write data: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return nil
}
