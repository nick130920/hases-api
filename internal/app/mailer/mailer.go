package mailer

import (
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
