package httpapi

import (
	"net/http"

	"github.com/google/uuid"
)

// listOverdueApplications lista postulaciones cuyo estado tiene SLA definida
// y cuya estancia en ese estado supera `max_days`. Usa `applications.updated_at`
// como aproximación al último cambio (el sistema graba ese campo en cada
// transición / patch).
func (s *Server) listOverdueApplications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT a.id, a.first_name, a.last_name, a.email, a.status,
		       a.updated_at::text,
		       sla.max_days,
		       EXTRACT(DAY FROM (now() - a.updated_at))::int AS days_in_state
		FROM applications a
		JOIN sla_definitions sla ON sla.state = a.status
		WHERE now() - a.updated_at > make_interval(days => sla.max_days)
		ORDER BY a.updated_at ASC
		LIMIT 200`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var fn, ln, em, st, updated string
		var maxDays, daysIn int
		_ = rows.Scan(&id, &fn, &ln, &em, &st, &updated, &maxDays, &daysIn)
		out = append(out, map[string]any{
			"id":            id.String(),
			"first_name":    fn,
			"last_name":     ln,
			"email":         em,
			"status":        st,
			"updated_at":    updated,
			"sla_max_days":  maxDays,
			"days_in_state": daysIn,
			"overdue_by":    daysIn - maxDays,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
