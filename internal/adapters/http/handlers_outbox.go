package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// listOutbox devuelve los mensajes en el outbox para inspección (admin).
// Filtros opcionales: ?status=pending|sent|failed&channel=email|whatsapp|sms
func (s *Server) listOutbox(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	channel := strings.TrimSpace(q.Get("channel"))
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, channel, to_address, subject, status, attempts, last_error,
		       scheduled_for::text, sent_at::text, created_at::text
		FROM outbox_messages
		WHERE ($1='' OR status=$1)
		  AND ($2='' OR channel=$2)
		ORDER BY created_at DESC
		LIMIT 200`, status, channel)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var ch, to, subj, st, lerr, sched, created string
		var sent pgtype.Text
		var attempts int
		_ = rows.Scan(&id, &ch, &to, &subj, &st, &attempts, &lerr, &sched, &sent, &created)
		out = append(out, map[string]any{
			"id":            id.String(),
			"channel":       ch,
			"to":            to,
			"subject":       subj,
			"status":        st,
			"attempts":      attempts,
			"last_error":    lerr,
			"scheduled_for": sched,
			"sent_at":       sent.String,
			"created_at":    created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) retryOutbox(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	if s.Notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifier not configured"})
		return
	}
	if err := s.Notifier.RequeueByID(r.Context(), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
