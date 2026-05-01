package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr           string
	DatabaseURL        string
	JWTSecret          string
	JWTExpiration      int // hours
	StorageDir         string
	UploadMaxBytes     int64
	UploadAllowMIME    []string
	CORSAllowedOrigins []string
	PortalBaseURL      string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	ResendAPIKey string
}

func Load() Config {
	jwtExp := envInt("JWT_EXPIRATION_HOURS", 72)
	dsn := envStr("DATABASE_URL", "postgres://hases:hases@localhost:5432/hases_rrhh?sslmode=disable")
	secret := envStr("JWT_SECRET", "dev-secret-change-in-production")
	addr := resolveHTTPAddr()
	storage := envStr("STORAGE_DIR", "./storage")

	uploadMax := int64(envInt("UPLOAD_MAX_BYTES", 25*1024*1024))
	allowed := envCSV("UPLOAD_ALLOWED_MIME",
		"application/pdf,image/jpeg,image/png,image/webp,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document")

	corsOrigins := envCSV("CORS_ALLOWED_ORIGINS", "http://localhost:4200,http://127.0.0.1:4200")

	return Config{
		HTTPAddr:           addr,
		DatabaseURL:        dsn,
		JWTSecret:          secret,
		JWTExpiration:      jwtExp,
		StorageDir:         storage,
		UploadMaxBytes:     uploadMax,
		UploadAllowMIME:    allowed,
		CORSAllowedOrigins: corsOrigins,
		PortalBaseURL:      envStr("PORTAL_BASE_URL", "http://localhost:4200"),

		SMTPHost: envStr("SMTP_HOST", ""),
		SMTPPort: envInt("SMTP_PORT", 587),
		SMTPUser: envStr("SMTP_USER", ""),
		SMTPPass: envStr("SMTP_PASS", ""),
		SMTPFrom: envStr("SMTP_FROM", ""),

		ResendAPIKey: envStr("RESEND_API_KEY", ""),
	}
}

// resolveHTTPAddr prefers HTTP_ADDR, otherwise builds ":$PORT" (Railway/Heroku style),
// finally defaults to ":8080".
func resolveHTTPAddr() string {
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		return v
	}
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

func envStr(k, d string) string {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	return v
}

func envInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func envCSV(k, d string) []string {
	raw := envStr(k, d)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AllowsMIME returns true if mime is in the allow-list (or list is empty).
func (c Config) AllowsMIME(mime string) bool {
	if len(c.UploadAllowMIME) == 0 {
		return true
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	for _, a := range c.UploadAllowMIME {
		if strings.EqualFold(a, mime) {
			return true
		}
	}
	return false
}
