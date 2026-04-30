package httpapi

import (
	"encoding/csv"
	"net/http"
	"strconv"
)

// exportApplicationsCSV: primera versión simple de reporte para F5.
// Genera CSV con todas las postulaciones (filtrable por status / vacancy_id).
func (s *Server) exportApplicationsCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.Pool.Query(r.Context(), `
		SELECT a.id, v.title AS vacancy, a.status,
		       a.first_name, a.last_name, a.email, a.phone,
		       a.channel, a.requires_vehicle, a.created_at
		FROM applications a
		JOIN vacancies v ON v.id = a.vacancy_id
		WHERE ($1 = '' OR a.status = $1)
		  AND ($2 = '' OR a.vacancy_id::text = $2)
		ORDER BY a.created_at DESC`,
		q.Get("status"), q.Get("vacancy_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="postulaciones.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"id", "vacante", "estado", "nombre", "apellido",
		"email", "telefono", "canal", "requiere_vehiculo", "creado",
	})
	for rows.Next() {
		var id, vac, st, fn, ln, em, ph, ch string
		var rv bool
		var created string
		if err := rows.Scan(&id, &vac, &st, &fn, &ln, &em, &ph, &ch, &rv, &created); err != nil {
			continue
		}
		_ = cw.Write([]string{id, vac, st, fn, ln, em, ph, ch, strconv.FormatBool(rv), created})
	}
}
