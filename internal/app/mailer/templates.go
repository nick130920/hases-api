package mailer

import (
	"fmt"
	"html"
	"strings"
)

// Template represents a transactional email body in both HTML and plain text.
// Built with deliberately conservative HTML/CSS so it renders consistently in
// Gmail, Outlook web, Apple Mail and the major Android/iOS clients.
type Template struct {
	HTML string
	Text string
}

// InvitationData feeds the worker portal invitation template.
type InvitationData struct {
	FullName string
	Link     string
	Token    string
	Days     int // days until the link expires
}

// HiringDecisionData feeds the hiring decision notification template.
type HiringDecisionData struct {
	FullName string
	Hired    bool
	Reason   string // optional, only for rejections
	Link     string // portal link (only when hired)
}

// RenderInvitation builds the worker portal invitation email.
func RenderInvitation(d InvitationData) Template {
	link := strings.TrimSpace(d.Link)
	greet := strings.TrimSpace(d.FullName)
	if greet == "" {
		greet = "Hola"
	} else {
		greet = "Hola " + greet
	}
	expiry := d.Days
	if expiry <= 0 {
		expiry = 7
	}

	htmlBody := layout("Invitación al portal HASES", fmt.Sprintf(`
		<p style="margin:0 0 16px">%s,</p>
		<p style="margin:0 0 16px">Has sido invitado al <strong>portal del trabajador HASES</strong> para continuar tu proceso de ingreso. El enlace es personal y caduca en %d días.</p>
		%s
		<p style="margin:0 0 8px;font-size:13px;color:#475569">Si el botón no funciona, copia y pega este enlace en tu navegador:</p>
		<p style="margin:0 0 24px;font-size:13px;color:#475569;word-break:break-all"><a href="%s" style="color:#1d4ed8">%s</a></p>
		<p style="margin:0 0 8px;font-size:13px;color:#475569">Código de invitación (por si lo solicita el portal):</p>
		<p style="margin:0 0 24px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;color:#0f172a">%s</p>
	`,
		html.EscapeString(greet),
		expiry,
		button(link, "Activar mi cuenta"),
		html.EscapeString(link),
		html.EscapeString(link),
		html.EscapeString(d.Token),
	))

	textBody := fmt.Sprintf(`%s,

Has sido invitado al portal del trabajador HASES para continuar tu proceso de ingreso.

Activa tu cuenta abriendo este enlace (caduca en %d días):
%s

Si tu cliente no detecta el enlace, este es el código de invitación: %s

Saludos,
Equipo HASES`, greet, expiry, link, d.Token)

	return Template{HTML: htmlBody, Text: textBody}
}

// RenderHiringDecision builds the email shown after the recruiter accepts or
// rejects a candidate.
func RenderHiringDecision(d HiringDecisionData) Template {
	greet := strings.TrimSpace(d.FullName)
	if greet == "" {
		greet = "Hola"
	} else {
		greet = "Hola " + greet
	}
	if d.Hired {
		htmlBody := layout("¡Bienvenido a HASES!", fmt.Sprintf(`
			<p style="margin:0 0 16px">%s,</p>
			<p style="margin:0 0 16px">Nos complace informarte que has sido <strong>aprobado para el cargo</strong>. El siguiente paso es completar tu ingreso desde el portal del trabajador.</p>
			%s
			<p style="margin:0 0 8px;font-size:13px;color:#475569">Si el botón no funciona, copia y pega este enlace en tu navegador:</p>
			<p style="margin:0 0 24px;font-size:13px;color:#475569;word-break:break-all"><a href="%s" style="color:#1d4ed8">%s</a></p>
		`, html.EscapeString(greet), button(d.Link, "Ir al portal"), html.EscapeString(d.Link), html.EscapeString(d.Link)))
		textBody := fmt.Sprintf("%s,\n\nNos complace informarte que has sido aprobado para el cargo. Continúa tu ingreso desde:\n%s\n\nSaludos,\nEquipo HASES", greet, d.Link)
		return Template{HTML: htmlBody, Text: textBody}
	}
	reason := strings.TrimSpace(d.Reason)
	reasonHTML := ""
	reasonText := ""
	if reason != "" {
		reasonHTML = fmt.Sprintf(`<p style="margin:0 0 16px;font-size:13px;color:#475569">Motivo: %s</p>`, html.EscapeString(reason))
		reasonText = "Motivo: " + reason + "\n\n"
	}
	htmlBody := layout("Resultado del proceso HASES", fmt.Sprintf(`
		<p style="margin:0 0 16px">%s,</p>
		<p style="margin:0 0 16px">Te informamos que tu candidatura no continúa en el proceso. Agradecemos tu interés y el tiempo dedicado.</p>
		%s
		<p style="margin:0 0 16px">Te deseamos mucho éxito en tu búsqueda profesional.</p>
	`, html.EscapeString(greet), reasonHTML))
	textBody := fmt.Sprintf("%s,\n\nTe informamos que tu candidatura no continúa en el proceso. Agradecemos tu interés y el tiempo dedicado.\n\n%sSaludos,\nEquipo HASES", greet, reasonText)
	return Template{HTML: htmlBody, Text: textBody}
}

// layout wraps body content with the standard HASES email shell.
func layout(title, body string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title>
</head>
<body style="margin:0;padding:0;background:#f1f5f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0f172a">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f1f5f9;padding:24px 16px">
    <tr><td align="center">
      <table role="presentation" width="560" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%%;background:#ffffff;border:1px solid #e2e8f0;border-radius:12px;overflow:hidden">
        <tr><td style="padding:24px 28px 8px;border-bottom:1px solid #e2e8f0">
          <p style="margin:0;font-size:13px;color:#1d4ed8;font-weight:600;letter-spacing:0.04em">HASES · RECURSOS HUMANOS</p>
          <h1 style="margin:6px 0 0;font-size:20px;color:#0f172a">%s</h1>
        </td></tr>
        <tr><td style="padding:24px 28px 8px;font-size:15px;line-height:1.55">
          %s
        </td></tr>
        <tr><td style="padding:16px 28px 28px;font-size:12px;color:#64748b;border-top:1px solid #e2e8f0">
          Este mensaje fue generado automáticamente por el sistema HASES. No respondas a este correo.
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), body)
}

// button renders a CSS-only button (table-based for Outlook compatibility).
func button(href, label string) string {
	return fmt.Sprintf(`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:8px 0 24px"><tr><td style="border-radius:8px;background:#1d4ed8"><a href="%s" target="_blank" style="display:inline-block;padding:12px 20px;font-size:14px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:8px">%s</a></td></tr></table>`,
		html.EscapeString(href), html.EscapeString(label))
}
