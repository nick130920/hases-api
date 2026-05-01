package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/hases/hases-api/internal/config"
)

// Mailer encapsula envío SMTP simple. Si no hay host configurado,
// es no-op y registra "skipped" (operación segura en dev).
type Mailer struct {
	cfg config.Config
}

func New(cfg config.Config) *Mailer {
	return &Mailer{cfg: cfg}
}

func (m *Mailer) Enabled() bool {
	return strings.TrimSpace(m.cfg.SMTPHost) != "" && strings.TrimSpace(m.cfg.SMTPFrom) != ""
}

// Send envía un email simple con cuerpo en texto plano.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)
	auth := smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.cfg.SMTPFrom, to, subject, body,
	))
	return smtp.SendMail(addr, auth, m.cfg.SMTPFrom, []string{to}, msg)
}

// SendWithAttachment envía un email multipart/mixed con un único adjunto.
// Útil para enviar PDFs (ej. examen ocupacional a la IPS).
func (m *Mailer) SendWithAttachment(to, subject, body, filename, mime string, data []byte) error {
	if !m.Enabled() {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)
	auth := smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)
	if mime == "" {
		mime = "application/octet-stream"
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
	fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", mime, filename)
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

	return smtp.SendMail(addr, auth, m.cfg.SMTPFrom, []string{to}, buf.Bytes())
}
