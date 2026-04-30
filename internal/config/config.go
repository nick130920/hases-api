package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	JWTSecret       string
	JWTExpiration   int // hours
	StorageDir      string
	UploadMaxBytes  int64
	UploadAllowMIME []string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string
}

func Load() Config {
	jwtExp := envInt("JWT_EXPIRATION_HOURS", 72)
	dsn := envStr("DATABASE_URL", "postgres://hases:hases@localhost:5432/hases_rrhh?sslmode=disable")
	secret := envStr("JWT_SECRET", "dev-secret-change-in-production")
	addr := envStr("HTTP_ADDR", ":8080")
	storage := envStr("STORAGE_DIR", "./storage")

	uploadMax := int64(envInt("UPLOAD_MAX_BYTES", 25*1024*1024))
	allowed := envCSV("UPLOAD_ALLOWED_MIME",
		"application/pdf,image/jpeg,image/png,image/webp,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document")

	return Config{
		HTTPAddr:        addr,
		DatabaseURL:     dsn,
		JWTSecret:       secret,
		JWTExpiration:   jwtExp,
		StorageDir:      storage,
		UploadMaxBytes:  uploadMax,
		UploadAllowMIME: allowed,

		SMTPHost: envStr("SMTP_HOST", ""),
		SMTPPort: envInt("SMTP_PORT", 587),
		SMTPUser: envStr("SMTP_USER", ""),
		SMTPPass: envStr("SMTP_PASS", ""),
		SMTPFrom: envStr("SMTP_FROM", ""),
	}
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
