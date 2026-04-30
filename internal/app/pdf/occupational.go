package pdf

import (
	"bytes"

	"github.com/jung-kurt/gofpdf"
)

// OccupationalExamPDF generates a simple pre-filled occupational exam PDF for IPS.
func OccupationalExamPDF(candidateName, documentID, vacancyTitle string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Examen ocupacional", false)
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, "Formato examen ocupacional (HASES RRHH)")
	pdf.Ln(12)
	pdf.SetFont("Arial", "", 11)
	pdf.Cell(0, 8, "Candidato: "+candidateName)
	pdf.Ln(6)
	pdf.Cell(0, 8, "Identificacion ref: "+documentID)
	pdf.Ln(6)
	pdf.Cell(0, 8, "Vacante / cargo: "+vacancyTitle)
	pdf.Ln(10)
	pdf.MultiCell(0, 6, "Este documento fue generado automaticamente por el sistema. Adjuntar y enviar a la IPS segun proceso interno.", "", "", false)

	var buf bytes.Buffer
	err := pdf.Output(&buf)
	return buf.Bytes(), err
}
