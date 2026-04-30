# Fase F5 — Backlog y decisiones

Esta fase queda como backlog pos-MVP. Aquí se documentan decisiones, alcance
sugerido y puntos de extensión ya disponibles en el código.

## Cola de correo robusta

Hoy `internal/app/mailer/mailer.go` envía email síncronamente a través de SMTP
si hay configuración (`SMTP_HOST`, `SMTP_FROM`). Es suficiente para
notificaciones simples (cambio de estado de postulación). Pasos sugeridos:

1. Persistir cada notificación pendiente en una tabla `outbox_emails`
   (id, to, subject, body, status, attempts, last_error).
2. Worker en background que recorre `pending` y reintenta con backoff.
3. Endpoint `GET /api/v1/admin/outbox` para inspección.

## Reportes exportables

Ya existe un primer reporte:

- `GET /api/v1/reports/applications.csv?status=...&vacancy_id=...`

Generación en `reports.go`. Plantillas adicionales sugeridas:

- Tiempo medio en cada estado del pipeline.
- Resultados IPS por mes (apto / no apto / restricciones).
- Onboarding completado por trabajador y fecha.

## WhatsApp asistido

Mantener WhatsApp por fuera del MVP por costo y soporte. Cuando se quiera
integrar, lo más práctico es:

1. Usar la API de WhatsApp Cloud (Meta) con plantillas pre-aprobadas.
2. Reaprovechar `mailer` con un `Notifier` interface (Email / WhatsApp / SMS)
   y un campo `channel` en `outbox_emails`.

## OCR

Solo si se valida el ROI:

- Procesar la cédula y antecedentes para extraer número y fecha.
- Tesseract o servicio gestionado (AWS Textract, GCP Vision).
- Punto de inyección: tras `uploadDocument`, encolar análisis y guardar
  campos extraídos junto al `application_document`.

## Auditoría y seguridad

- Revisar y completar `audit_logs.payload` con diffs serializados (hoy es NULL).
- Considerar rate limiting en `/auth/login` y endpoints públicos.
- Rotación periódica del JWT secret y soporte para tokens opacos en revocación.

## Performance

- Indexar `application_documents.application_id` (ya existe vía FK + unique
  composite). Considerar índice GIN sobre `audit_logs.payload` si se llena.
- Evaluar paginación real (`limit`/`offset` o cursor) en `listApplications`,
  `listAuditLogs` cuando los volúmenes crezcan.
