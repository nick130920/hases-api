package mailer

import (
	"fmt"
	"html"
	"strings"
)

// Template represents a transactional email body in both HTML and plain text.
// Built with deliberately conservative HTML/CSS so it renders consistently in
// Gmail, Outlook web, Apple Mail and the major Android/iOS clients. La paleta
// y la tipografia siguen el sistema de diseno HASES (web/DESIGN.md).
type Template struct {
	HTML string
	Text string
}

// Tokens del design system (web/DESIGN.md). Mantenerlos centralizados evita
// drift visual entre la app y los correos.
const (
	colorPrimary       = "#0e5e54"
	colorPrimaryHover  = "#0a4a42"
	colorPrimaryMuted  = "#cfe8e4"
	colorAccent        = "#2d8a54"
	colorAccentSoft    = "#e8f5ec"
	colorSurface       = "#eef3f1"
	colorSurfaceCard   = "#ffffff"
	colorOnSurface     = "#132221"
	colorOnSurfaceMute = "#4a5f5c"
	colorOnHeader      = "#f4faf9"
	colorOutline       = "#b8cec9"
	colorLink          = "#0a6b5f"
	fontStack          = "'Plus Jakarta Sans','Segoe UI',-apple-system,BlinkMacSystemFont,Roboto,Helvetica,Arial,sans-serif"
)

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

// StatusUpdateData feeds the generic pipeline status notification email.
type StatusUpdateData struct {
	FullName    string
	StatusLabel string // etiqueta legible (ej. "Documentos pendientes")
	Status      string // codigo crudo, opcional, se muestra como referencia
	Message     string // copy adicional opcional para guiar al candidato
	Link        string // CTA opcional (ej. enlace al portal)
	LinkLabel   string // texto del CTA opcional
}

// RenderInvitation builds the worker portal invitation email.
func RenderInvitation(d InvitationData) Template {
	link := strings.TrimSpace(d.Link)
	greet := salutation(d.FullName)
	expiry := d.Days
	if expiry <= 0 {
		expiry = 7
	}

	body := fmt.Sprintf(`
		<p style="margin:0 0 12px;%s">%s,</p>
		<p style="margin:0 0 20px;%s">Has sido invitado al <strong>portal del trabajador HASES</strong> para continuar tu proceso de ingreso. Allí podrás cargar documentos, firmar formatos y avanzar en la inducción.</p>
		%s
		<p style="margin:24px 0 6px;%s">Tu enlace personal es válido durante <strong>%d días</strong>. Si el botón no funciona, copia esta URL en tu navegador:</p>
		<p style="margin:0 0 20px;word-break:break-all"><a href="%s" style="color:%s;font-weight:600;text-decoration:underline">%s</a></p>
		%s
	`,
		styleBody(), html.EscapeString(greet),
		styleBody(),
		button(link, "Activar mi cuenta"),
		styleMuted(), expiry,
		html.EscapeString(link), colorLink, html.EscapeString(link),
		infoBlock("Código de invitación", d.Token),
	)

	textBody := fmt.Sprintf(`%s,

Has sido invitado al portal del trabajador HASES para continuar tu proceso de ingreso.

Activa tu cuenta abriendo este enlace (válido %d días):
%s

Si tu cliente no detecta el enlace, este es el código de invitación:
%s

Saludos,
Equipo HASES`, greet, expiry, link, d.Token)

	return Template{HTML: layout("Invitación al portal HASES", "Continúa tu ingreso", body), Text: textBody}
}

// RenderHiringDecision builds the email shown after the recruiter accepts or
// rejects a candidate.
func RenderHiringDecision(d HiringDecisionData) Template {
	greet := salutation(d.FullName)
	if d.Hired {
		body := fmt.Sprintf(`
			%s
			<p style="margin:24px 0 12px;%s">%s,</p>
			<p style="margin:0 0 20px;%s">Nos complace informarte que has sido <strong>aprobado para el cargo</strong>. El siguiente paso es completar tu ingreso desde el portal del trabajador.</p>
			%s
			<p style="margin:24px 0 6px;%s">Si el botón no funciona, copia esta URL en tu navegador:</p>
			<p style="margin:0 0 8px;word-break:break-all"><a href="%s" style="color:%s;font-weight:600;text-decoration:underline">%s</a></p>
		`,
			highlightBanner(colorAccentSoft, colorAccent, "Resultado del proceso", "¡Bienvenido al equipo HASES!"),
			styleBody(), html.EscapeString(greet),
			styleBody(),
			button(d.Link, "Ir al portal del trabajador"),
			styleMuted(),
			html.EscapeString(d.Link), colorLink, html.EscapeString(d.Link),
		)
		text := fmt.Sprintf(`%s,

Nos complace informarte que has sido aprobado para el cargo. Continúa tu ingreso desde el portal:
%s

Saludos,
Equipo HASES`, greet, d.Link)
		return Template{HTML: layout("¡Bienvenido a HASES!", "Has sido contratado", body), Text: text}
	}

	reason := strings.TrimSpace(d.Reason)
	reasonHTML := ""
	reasonText := ""
	if reason != "" {
		reasonHTML = infoBlock("Motivo", reason)
		reasonText = "Motivo: " + reason + "\n\n"
	}
	body := fmt.Sprintf(`
		<p style="margin:0 0 12px;%s">%s,</p>
		<p style="margin:0 0 20px;%s">Te informamos que tu candidatura no continúa en el proceso. Agradecemos sinceramente tu interés y el tiempo dedicado durante esta etapa.</p>
		%s
		<p style="margin:24px 0 0;%s">Te deseamos mucho éxito en tus próximos pasos profesionales.</p>
	`, styleBody(), html.EscapeString(greet), styleBody(), reasonHTML, styleBody())
	text := fmt.Sprintf(`%s,

Te informamos que tu candidatura no continúa en el proceso. Agradecemos tu interés y el tiempo dedicado.

%sSaludos,
Equipo HASES`, greet, reasonText)
	return Template{HTML: layout("Resultado del proceso HASES", "Resultado del proceso", body), Text: text}
}

// RenderStatusUpdate builds the email sent when the pipeline status of an
// application changes (ej. paso a docs_pending o interview_pending). El estado
// se muestra como badge en lugar del codigo crudo, alineado a la marca.
func RenderStatusUpdate(d StatusUpdateData) Template {
	greet := salutation(d.FullName)
	statusLabel := strings.TrimSpace(d.StatusLabel)
	if statusLabel == "" {
		statusLabel = strings.TrimSpace(d.Status)
	}
	if statusLabel == "" {
		statusLabel = "Actualización en curso"
	}

	intro := strings.TrimSpace(d.Message)
	if intro == "" {
		intro = "Te escribimos para informarte que tu proceso de selección ha avanzado a una nueva etapa. Si necesitas información adicional, contáctanos respondiendo a la persona que gestiona tu postulación."
	}

	cta := ""
	if link := strings.TrimSpace(d.Link); link != "" {
		label := strings.TrimSpace(d.LinkLabel)
		if label == "" {
			label = "Ir al portal"
		}
		cta = button(link, label)
	}

	body := fmt.Sprintf(`
		<p style="margin:0 0 12px;%s">%s,</p>
		<p style="margin:0 0 20px;%s">%s</p>
		%s
		%s
	`,
		styleBody(), html.EscapeString(greet),
		styleBody(), html.EscapeString(intro),
		statusBadge(statusLabel),
		cta,
	)

	rawCode := ""
	if c := strings.TrimSpace(d.Status); c != "" && c != statusLabel {
		rawCode = " (" + c + ")"
	}
	text := fmt.Sprintf(`%s,

%s

Estado actual: %s%s

Saludos,
Equipo HASES`, greet, intro, statusLabel, rawCode)
	if l := strings.TrimSpace(d.Link); l != "" {
		text += "\n\nPortal: " + l
	}

	return Template{HTML: layout("Actualización de tu postulación · HASES", "Actualización de tu postulación", body), Text: text}
}

// ---- helpers compartidos ----

func salutation(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "Hola"
	}
	return "Hola " + n
}

func styleBody() string {
	return fmt.Sprintf("font-size:15px;line-height:1.6;color:%s;font-family:%s", colorOnSurface, fontStack)
}

func styleMuted() string {
	return fmt.Sprintf("font-size:13px;line-height:1.55;color:%s;font-family:%s", colorOnSurfaceMute, fontStack)
}

// statusBadge produce un chip visual con la etiqueta del estado actual.
func statusBadge(label string) string {
	return fmt.Sprintf(`
		<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 4px">
		  <tr><td style="background:%s;border:1px solid %s;border-radius:9999px;padding:8px 16px">
		    <span style="font-family:%s;font-size:12px;font-weight:600;letter-spacing:0.04em;text-transform:uppercase;color:%s">Estado actual</span>
		    <span style="font-family:%s;font-size:14px;font-weight:600;color:%s;margin-left:8px">%s</span>
		  </td></tr>
		</table>`,
		colorPrimaryMuted, colorOutline,
		fontStack, colorPrimary,
		fontStack, colorPrimary, html.EscapeString(label),
	)
}

// infoBlock renderiza una caja sutil con etiqueta + valor (codigos, motivos).
func infoBlock(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return fmt.Sprintf(`
		<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%" style="margin:0 0 16px">
		  <tr><td style="background:%s;border:1px solid %s;border-radius:10px;padding:14px 16px">
		    <p style="margin:0 0 4px;font-family:%s;font-size:11px;font-weight:600;letter-spacing:0.06em;text-transform:uppercase;color:%s">%s</p>
		    <p style="margin:0;font-family:ui-monospace,'SFMono-Regular',Menlo,Consolas,monospace;font-size:13px;color:%s;word-break:break-all">%s</p>
		  </td></tr>
		</table>`,
		colorAccentSoft, colorOutline,
		fontStack, colorPrimary, html.EscapeString(label),
		colorOnSurface, html.EscapeString(value),
	)
}

// highlightBanner produce una franja colorida para anunciar resultados positivos
// (ej. "¡Bienvenido al equipo HASES!").
func highlightBanner(bg, accent, eyebrow, title string) string {
	return fmt.Sprintf(`
		<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%" style="margin:8px 0 4px">
		  <tr><td style="background:%s;border-left:4px solid %s;border-radius:10px;padding:18px 20px">
		    <p style="margin:0 0 4px;font-family:%s;font-size:11px;font-weight:600;letter-spacing:0.06em;text-transform:uppercase;color:%s">%s</p>
		    <p style="margin:0;font-family:%s;font-size:18px;font-weight:700;letter-spacing:-0.01em;color:%s">%s</p>
		  </td></tr>
		</table>`,
		bg, accent,
		fontStack, accent, html.EscapeString(eyebrow),
		fontStack, colorPrimary, html.EscapeString(title),
	)
}

// layout wraps body content with the standard HASES email shell.
//
// Usa una tabla principal centrada (compatible con Outlook) sobre un fondo
// `surface` HASES, con cabecera teal de marca y franja decorativa accent.
func layout(documentTitle, sectionTitle, body string) string {
	preheader := html.EscapeString(sectionTitle)
	return fmt.Sprintf(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="x-apple-disable-message-reformatting">
<title>%s</title>
</head>
<body style="margin:0;padding:0;background:%s;font-family:%s;color:%s">
  <span style="display:none!important;visibility:hidden;opacity:0;color:transparent;height:0;width:0;overflow:hidden">%s</span>
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:%s;padding:32px 16px">
    <tr><td align="center">
      <table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%%;background:%s;border:1px solid %s;border-radius:14px;overflow:hidden;box-shadow:0 8px 28px rgba(14,94,84,0.10)">
        <tr><td style="background:%s;padding:18px 28px">
          <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
            <tr>
              <td style="vertical-align:middle">
                <p style="margin:0;font-family:%s;font-size:18px;font-weight:700;letter-spacing:-0.01em;color:%s">HASES <span style="font-weight:500;opacity:0.85">Ingeniería</span></p>
                <p style="margin:2px 0 0;font-family:%s;font-size:11px;font-weight:500;letter-spacing:0.06em;text-transform:uppercase;color:%s">Recursos humanos · Neiva, Huila</p>
              </td>
              <td align="right" style="vertical-align:middle">
                <span style="display:inline-block;padding:6px 12px;background:rgba(255,255,255,0.12);border:1px solid rgba(255,255,255,0.25);border-radius:9999px;font-family:%s;font-size:11px;font-weight:600;letter-spacing:0.04em;text-transform:uppercase;color:%s">Operación en verde</span>
              </td>
            </tr>
          </table>
        </td></tr>
        <tr><td style="height:4px;background:%s;line-height:4px;font-size:0">&nbsp;</td></tr>
        <tr><td style="padding:32px 32px 16px">
          <p style="margin:0 0 4px;font-family:%s;font-size:12px;font-weight:600;letter-spacing:0.06em;text-transform:uppercase;color:%s">Notificación HASES</p>
          <h1 style="margin:0 0 24px;font-family:%s;font-size:24px;line-height:1.25;font-weight:700;letter-spacing:-0.02em;color:%s">%s</h1>
          %s
        </td></tr>
        <tr><td style="padding:20px 32px 28px;border-top:1px solid %s">
          <p style="margin:0 0 4px;font-family:%s;font-size:12px;font-weight:600;color:%s">HASES Ingeniería S.A.S.</p>
          <p style="margin:0;font-family:%s;font-size:11px;line-height:1.5;color:%s">Aseo · Jardinería · Piscinas · Apoyo operativo · Sostenibilidad<br>Este es un correo automático del sistema interno de RR.HH., por favor no respondas a esta dirección.</p>
        </td></tr>
      </table>
      <p style="margin:16px 0 0;font-family:%s;font-size:11px;color:%s">© HASES Ingeniería · Sistema de gestión humana</p>
    </td></tr>
  </table>
</body>
</html>`,
		html.EscapeString(documentTitle),
		colorSurface, fontStack, colorOnSurface,
		preheader,
		colorSurface,
		colorSurfaceCard, colorOutline,
		colorPrimary,
		fontStack, colorOnHeader,
		fontStack, colorPrimaryMuted,
		fontStack, colorOnHeader,
		colorAccent,
		fontStack, colorAccent,
		fontStack, colorOnSurface, html.EscapeString(sectionTitle),
		body,
		colorOutline,
		fontStack, colorPrimary,
		fontStack, colorOnSurfaceMute,
		fontStack, colorOnSurfaceMute,
	)
}

// button renders a CSS-only button (table-based for Outlook compatibility) que
// adopta los colores `primary` / `on-primary` del design system.
func button(href, label string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	return fmt.Sprintf(`
		<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 4px">
		  <tr><td style="background:%s;border-radius:10px">
		    <a href="%s" target="_blank" rel="noopener" style="display:inline-block;padding:14px 26px;font-family:%s;font-size:14px;font-weight:700;letter-spacing:0.02em;color:%s;text-decoration:none;border-radius:10px">%s</a>
		  </td></tr>
		</table>`,
		colorPrimary, html.EscapeString(href),
		fontStack, "#ffffff", html.EscapeString(label),
	)
}
