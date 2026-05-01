package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// sendTestEmail dispara un correo sincrono para validar credenciales SMTP.
// Endpoint reservado a admin para usarse durante despliegues y debug.
func (s *Server) sendTestEmail(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin only"})
		return
	}
	var body struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.To = strings.TrimSpace(body.To)
	if body.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to is required"})
		return
	}
	if body.Subject == "" {
		body.Subject = "HASES SMTP test"
	}
	if body.Body == "" {
		body.Body = "Este es un correo de prueba enviado desde el API HASES para validar las credenciales SMTP."
	}
	if !s.Mailer.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "smtp disabled"})
		return
	}
	if err := s.Mailer.Send(body.To, body.Subject, body.Body); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "to": body.To})
}
