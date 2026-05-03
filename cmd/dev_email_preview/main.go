// Comando interno para previsualizar las plantillas de correo localmente.
// Ejecutar: `go run ./cmd/dev_email_preview` y abrir los .html en el navegador.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hases/hases-api/internal/app/mailer"
	"github.com/hases/hases-api/internal/domain"
)

func main() {
	out := filepath.Join("tmp", "email-preview")
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cases := map[string]mailer.Template{
		"status_docs_pending.html": mailer.RenderStatusUpdate(mailer.StatusUpdateData{
			FullName:    "Nicolás Cárdenas",
			StatusLabel: domain.StatusLabel(domain.StatusDocsPending),
			Status:      domain.StatusDocsPending,
			Message:     "Pasamos a la etapa de carga documental. Por favor revisa el listado de documentos requeridos y súbelos cuando los tengas listos.",
			Link:        "https://hases-web-production.up.railway.app/portal/documentos",
			LinkLabel:   "Ir al portal del trabajador",
		}),
		"invitation.html": mailer.RenderInvitation(mailer.InvitationData{
			FullName: "Nicolás Cárdenas",
			Link:     "https://hases-web-production.up.railway.app/portal/aceptar-invitacion?token=abc123",
			Token:    "7fc2887b9d13af1f3216b57a8680a56e6176dcf1bef22e80",
			Days:     7,
		}),
		"hire.html": mailer.RenderHiringDecision(mailer.HiringDecisionData{
			FullName: "Nicolás Cárdenas",
			Hired:    true,
			Link:     "https://hases-web-production.up.railway.app/portal/inicio",
		}),
		"reject.html": mailer.RenderHiringDecision(mailer.HiringDecisionData{
			FullName: "Nicolás Cárdenas",
			Hired:    false,
			Reason:   "El perfil no se ajusta a los requisitos técnicos del cargo en esta convocatoria.",
		}),
	}

	for name, tpl := range cases {
		if err := os.WriteFile(filepath.Join(out, name), []byte(tpl.HTML), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("Previews escritos en %s\n", out)
}
