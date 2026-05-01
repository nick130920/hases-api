package httpapi

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// exportPipelineTimeCSV calcula el tiempo (días promedio) entre la creación
// de la postulación y el último cambio de estado, agrupado por estado actual.
// Es una vista simple basada en `audit_logs.action LIKE 'transition:%'`.
func (s *Server) exportPipelineTimeCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		WITH transitions AS (
			SELECT
				al.entity_id AS application_id,
				SUBSTRING(al.action FROM 'transition:(.*)') AS to_status,
				al.created_at,
				LAG(al.created_at) OVER (PARTITION BY al.entity_id ORDER BY al.created_at) AS previous_at
			FROM audit_logs al
			WHERE al.entity_type='application' AND al.action LIKE 'transition:%'
		)
		SELECT to_status,
		       COUNT(*)                                    AS samples,
		       ROUND(AVG(EXTRACT(EPOCH FROM (created_at - previous_at)))/86400, 2) AS avg_days,
		       ROUND(MIN(EXTRACT(EPOCH FROM (created_at - previous_at)))/86400, 2) AS min_days,
		       ROUND(MAX(EXTRACT(EPOCH FROM (created_at - previous_at)))/86400, 2) AS max_days
		FROM transitions
		WHERE previous_at IS NOT NULL
		GROUP BY to_status
		ORDER BY samples DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pipeline-time.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"estado", "muestras", "promedio_dias", "min_dias", "max_dias"})
	for rows.Next() {
		var status string
		var samples int
		var avg, min, max float64
		if err := rows.Scan(&status, &samples, &avg, &min, &max); err != nil {
			continue
		}
		_ = cw.Write([]string{
			status,
			strconv.Itoa(samples),
			strconv.FormatFloat(avg, 'f', 2, 64),
			strconv.FormatFloat(min, 'f', 2, 64),
			strconv.FormatFloat(max, 'f', 2, 64),
		})
	}
}

// exportIPSMonthlyCSV: conteo apto/no apto/restricciones por mes.
func (s *Server) exportIPSMonthlyCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT to_char(date_trunc('month', recorded_at), 'YYYY-MM') AS mes,
		       SUM(CASE WHEN outcome='fit' THEN 1 ELSE 0 END)              AS apto,
		       SUM(CASE WHEN outcome='unfit' THEN 1 ELSE 0 END)            AS no_apto,
		       SUM(CASE WHEN outcome='fit_restrictions' THEN 1 ELSE 0 END) AS restricciones,
		       COUNT(*)                                                    AS total
		FROM ips_results
		GROUP BY 1
		ORDER BY 1 DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ips-mensual.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"mes", "apto", "no_apto", "restricciones", "total"})
	for rows.Next() {
		var mes string
		var apto, noApto, rest, total int
		if err := rows.Scan(&mes, &apto, &noApto, &rest, &total); err != nil {
			continue
		}
		_ = cw.Write([]string{mes, strconv.Itoa(apto), strconv.Itoa(noApto), strconv.Itoa(rest), strconv.Itoa(total)})
	}
}

// exportOnboardingCompletedCSV: trabajadores con onboarding cerrado entre
// `from` y `to` (formato YYYY-MM-DD). Si faltan, usa últimos 6 meses.
func (s *Server) exportOnboardingCompletedCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := strings.TrimSpace(q.Get("from"))
	to := strings.TrimSpace(q.Get("to"))
	if from == "" {
		from = time.Now().AddDate(0, -6, 0).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT a.id, a.first_name, a.last_name, a.email, v.title,
		       fp.onboarding_completed_at::text
		FROM functional_plans fp
		JOIN applications a ON a.id = fp.application_id
		JOIN vacancies v ON v.id = a.vacancy_id
		WHERE fp.onboarding_completed_at >= $1::date
		  AND fp.onboarding_completed_at < $2::date
		ORDER BY fp.onboarding_completed_at DESC`, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="onboarding-completados.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"id", "nombre", "apellido", "email", "vacante", "fecha_cierre"})
	for rows.Next() {
		var id, fn, ln, em, vac, dt string
		if err := rows.Scan(&id, &fn, &ln, &em, &vac, &dt); err != nil {
			continue
		}
		_ = cw.Write([]string{id, fn, ln, em, vac, dt})
	}
}
