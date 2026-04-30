package httpapi

import (
	"context"
	"net/http"
	"strings"

	appauth "github.com/hases/hases-api/internal/auth"
)

type ctxKey string

const claimsKey ctxKey = "jwt_claims"

func BearerAuth(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" || !strings.HasPrefix(strings.ToLower(h), "bearer ") {
			http.Error(w, `{"error":"no bearer token"}`, http.StatusUnauthorized)
			return
		}
		raw := strings.TrimSpace(h[7:])
		c, err := appauth.Parse(secret, raw)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClaimsFromCtx(r *http.Request) *appauth.Claims {
	v := r.Context().Value(claimsKey)
	cl, ok := v.(*appauth.Claims)
	if !ok {
		return nil
	}
	return cl
}

func RequireRoles(r *http.Request, allowed ...string) bool {
	cl := ClaimsFromCtx(r)
	if cl == nil {
		return false
	}
	for _, a := range allowed {
		if cl.Role == a {
			return true
		}
	}
	return false
}
