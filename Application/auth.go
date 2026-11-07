package application

import (
	"context"
	"net/http"
	"strings"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// userIDKey is a context key used to store the user ID in the request context.
type userIDKey struct{}

// RequireAuth is a middleware that ensures the request has a valid Bearer token.
func RequireAuth(verifier domain.TokenVerifier, clock domain.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") || token == "" {
				writeUnauthorized(w)
				return
			}

			userID, err := verifier.Verify(token, clock.Now())
			if err != nil {
				writeUnauthorized(w)
				return
			}
			ctx := r.Context()
			ctx = context.WithValue(ctx, userIDKey{}, userID)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromContext retrieves the user ID from the context, if available. It returns the user ID and a boolean indicating whether it was found.
func UserIDFromContext(ctx context.Context) (domain.UserID, bool) {
	userID, ok := ctx.Value(userIDKey{}).(domain.UserID)
	return userID, ok
}

// writeUnauthorized writes a 401 Unauthorized response with the appropriate WWW-Authenticate header.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid credentials or token")
}
