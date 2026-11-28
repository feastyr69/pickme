package middleware

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// BrowserCORS permits the configured frontend origin to call the API.
func BrowserCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("WEB_ORIGIN")
		if origin == "" {
			origin = "http://localhost:3000"
		}
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions && strings.EqualFold(r.Header.Get("Origin"), origin) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SupabaseJWTVerifier validates Supabase ES256 access tokens against the
// project's remotely published signing keys.
type SupabaseJWTVerifier struct {
	keys   keyfunc.Keyfunc
	cancel context.CancelFunc
}

type userIDContextKey struct{}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey{}).(string)
	return userID, ok && userID != ""
}

func NewSupabaseJWTVerifier(supabaseURL string) (*SupabaseJWTVerifier, error) {
	supabaseURL = strings.TrimRight(supabaseURL, "/")
	if supabaseURL == "" {
		return nil, errors.New("SUPABASE_URL is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	keys, err := keyfunc.NewDefaultCtx(ctx, []string{supabaseURL + "/auth/v1/.well-known/jwks.json"})
	if err != nil {
		cancel()
		return nil, err
	}
	return &SupabaseJWTVerifier{keys: keys, cancel: cancel}, nil
}

func (v *SupabaseJWTVerifier) Close() {
	v.cancel()
}

func (v *SupabaseJWTVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.Fields(r.Header.Get("Authorization"))
		if len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		claims := jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(
			auth[1],
			&claims,
			v.keys.Keyfunc,
			jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
			jwt.WithAudience("authenticated"),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Subject == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDContextKey{}, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
