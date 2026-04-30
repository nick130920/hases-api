# HASES RR.HH. — API

API REST que soporta el ciclo de vida del trabajador en HASES: contratación,
documentos, entrevistas, examen ocupacional, contrato e inducción
organizacional y funcional, hasta cierre del onboarding.

- **Lenguaje:** Go 1.22+
- **Base de datos:** PostgreSQL 16
- **Auth:** JWT (Bearer)
- **Router:** chi v5
- **Migraciones:** goose embebido
- **PDF:** gofpdf
- **Spec:** OpenAPI 3 servido en runtime

## Estructura

```
hases-api/
├── cmd/api/                    # Entry point del servicio HTTP
├── internal/
│   ├── adapters/
│   │   ├── http/               # Server, rutas, middleware, handlers
│   │   └── persistence/        # Pool pgx + migraciones embebidas
│   ├── app/
│   │   ├── mailer/             # SMTP opcional para notificaciones
│   │   └── pdf/                # Generación PDF (examen ocupacional)
│   ├── auth/                   # Hashing y firma JWT
│   ├── config/                 # Carga de configuración por env
│   └── domain/                 # Constantes del pipeline
├── openapi/                    # OpenAPI 3 (autoritativo, embebido)
└── docs/                       # Notas y backlog
```

## Configuración

Copia `.env.example` a `.env` y ajusta. Variables soportadas:

| Variable | Default | Descripción |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Dirección de escucha |
| `DATABASE_URL` | `postgres://hases:hases@localhost:5432/hases_rrhh?sslmode=disable` | DSN de PostgreSQL |
| `JWT_SECRET` | `dev-secret-change-in-production` | Secreto para firmar JWT |
| `JWT_EXPIRATION_HOURS` | `72` | Horas de validez del token |
| `STORAGE_DIR` | `./storage` | Carpeta para archivos subidos |
| `ADMIN_EMAIL` | `admin@local.test` | Email del admin sembrado en arranque |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | Password inicial del admin |
| `UPLOAD_MAX_BYTES` | `26214400` | Límite por archivo (25 MB) |
| `UPLOAD_ALLOWED_MIME` | PDF/JPEG/PNG/WEBP/DOC/DOCX | Lista CSV de MIME permitidos |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` | — | SMTP opcional. Si `SMTP_HOST` y `SMTP_FROM` están seteados se notifican los cambios de estado de postulación |

> En producción, **siempre** sobreescribir `JWT_SECRET` y la contraseña inicial.

## Ejecutar en local

Necesitas Docker (para PostgreSQL) y Go 1.22+.

```bash
# 1. Levantar la base de datos
docker compose -f ../docker/docker-compose.yml up -d   # opcional, ver hases-rrhh
# o tu propio Postgres apuntando a DATABASE_URL

# 2. Migraciones + admin se aplican al arrancar el servicio
go run ./cmd/api
```

El servicio:

1. Aplica migraciones (`internal/adapters/persistence/migrations/*.sql`).
2. Crea el usuario admin si no existe ningún usuario.
3. Sirve el API en `HTTP_ADDR`.

### Probar

```bash
# Health
curl http://localhost:8080/health

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@local.test","password":"admin123"}'

# Spec OpenAPI
curl http://localhost:8080/openapi.yaml
```

## Endpoints principales

Especificación completa en `openapi/openapi.yaml` (también servida en
`GET /openapi.yaml`). Resumen por dominio:

- **Auth / meta**: `POST /auth/login`, `GET /me`, `GET /health`.
- **Vacantes**: CRUD, `POST /vacancies/{id}/publish`, `POST /vacancies/{id}/archive`.
- **Postulaciones**: lista filtrable (`vacancy_id`, `status`, `q`),
  detalle con documentos y completitud, transición de estado.
- **Documentos**: subida multipart con allow-list de MIME y revisión
  (`approved` / `rejected`) con notas y revisor.
- **Entrevistas**: plantillas, preguntas, sesiones por postulación,
  edición de programación y `PUT` de respuestas.
- **Ocupacional / IPS**: PDF prellenado, registro de envío y de resultado.
- **Inducción organizacional**: módulos, marcado de avance, firmas
  multipart (reglamento, políticas, contrato), cierre.
- **Funcional / EPP**: plan, cierre de teoría, entrega EPP con firma,
  `practice-start` (requiere firma EPP), evidencias con archivos, cierre.
- **Catálogos**: motivos de rechazo (CRUD).
- **Usuarios**: list/create/patch/desactivar (rol `admin`).
- **Reportes**: `GET /reports/applications.csv?status=&vacancy_id=`.
- **Auditoría**: `GET /audit-logs` con últimas 200 acciones.

## Roles

`admin`, `hr`, `evaluator`, `hiring_manager`. Algunas operaciones de
administración exigen `admin` (usuarios) o `admin|hr` (catálogos).

## Migraciones

Embebidas con goose en `internal/adapters/persistence/migrations`.
Para agregar una nueva, crea `00X_descripcion.sql` con las secciones
`-- +goose Up` y `-- +goose Down`.

## Build y verificación

```bash
go build ./...
go vet ./...
```

## Backlog Fase 5

Notas de cola de email robusta, reportería, WhatsApp y OCR en
[`docs/BACKLOG-F5.md`](docs/BACKLOG-F5.md).

## Licencia

Uso interno HASES.
