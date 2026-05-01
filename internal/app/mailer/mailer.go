package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/hases/hases-api/internal/config"
)

// Mailer envía correo transaccional. Se mantiene como struct (no interfaz)
// para no romper a los consumidores; internamente elige el transporte:
//
//   - Resend HTTP API si RESEND_API_KEY está definido (recomendado en
//     plataformas como Railway que bloquean SMTP saliente).
//   - SMTP en caso contrario, con dos modos:
//   - Implicit TLS (SMTPS) cuando el puerto es 465.
//   - STARTTLS / texto plano para puertos 587 o 25.
//
// Cuando ningún transporte está configurado, los métodos son no-op.
type Mailer struct {
	cfg    config.Config
	client *http.Client
}

func New(cfg config.Config) *Mailer {
	return &Mailer{
		cfg:    cfg,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (m *Mailer) usingResend() bool {
	return strings.TrimSpace(m.cfg.ResendAPIKey) != ""
}

func (m *Mailer) usingSMTP() bool {
	return strings.TrimSpace(m.cfg.SMTPHost) != ""
}

// Enabled reports whether at least one transport plus a From address are set.
func (m *Mailer) Enabled() bool {
	if strings.TrimSpace(m.cfg.SMTPFrom) == "" {
		return false
	}
	return m.usingResend() || m.usingSMTP()
}

// envelopeFrom returns just the email address from SMTP_FROM. Used as MAIL
// FROM in SMTP envelopes, which require a bare address even when SMTP_FROM
// carries a display name like "HASES RR.HH. <hr@example.com>".
func (m *Mailer) envelopeFrom() string {
	addr, err := mail.ParseAddress(m.cfg.SMTPFrom)
	if err != nil {
		return strings.TrimSpace(m.cfg.SMTPFrom)
	}
	return addr.Address
}

// Send envía un email simple en texto plano.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		return nil
	}
	if m.usingResend() {
		return m.sendViaResend(to, subject, body, nil)
	}
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.cfg.SMTPFrom, to, subject, body,
	))
	return m.deliverSMTP(to, msg)
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
	if m.usingResend() {
		return m.sendViaResend(to, subject, body, &resendAttachment{
			Filename:    filename,
			Content:     base64.StdEncoding.EncodeToString(data),
			ContentType: mimeType,
		})
	}
	return m.deliverSMTP(to, m.buildMultipart(to, subject, body, filename, mimeType, data))
}

// ---- Resend HTTP API ----

type resendAttachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

type resendPayload struct {
	From        string              `json:"from"`
	To          []string            `json:"to"`
	Subject     string              `json:"subject"`
	Text        string              `json:"text,omitempty"`
	Attachments []*resendAttachment `json:"attachments,omitempty"`
}

func (m *Mailer) sendViaResend(to, subject, body string, attach *resendAttachment) error {
	payload := resendPayload{
		From:    m.cfg.SMTPFrom,
		To:      []string{to},
		Subject: subject,
		Text:    body,
	}
	if attach != nil {
		payload.Attachments = []*resendAttachment{attach}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.cfg.ResendAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend dial: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// ---- SMTP transport ----

func (m *Mailer) buildMultipart(to, subject, body, filename, mimeType string, data []byte) []byte {
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
	return buf.Bytes()
}

func (m *Mailer) deliverSMTP(to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)
	auth := smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)
	from := m.envelopeFrom()

	if m.cfg.SMTPPort == 465 {
		return m.sendImplicitTLS(addr, auth, from, []string{to}, msg)
	}
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// sendImplicitTLS opens a direct TLS connection (SMTPS) and runs the SMTP
// dialogue manually. Required for relays that only listen on port 465.
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
