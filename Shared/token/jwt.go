package token

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

var _ domain.TokenIssuer = (*JWT)(nil)
var _ domain.TokenVerifier = (*JWT)(nil)

// JWT represents a JSON Web Token (JWT) issuer and verifier.
type JWT struct {
	secret []byte
	ttl    time.Duration
}

// NewJWT creates a new JWT instance with the given secret and time-to-live (TTL).
func NewJWT(secret []byte, ttl time.Duration) *JWT {
	return &JWT{
		secret: secret,
		ttl:    ttl,
	}
}

// Issue generates a new JWT token for the given user ID with the specified expiration time.
func (j *JWT) Issue(userID domain.UserID, now time.Time) (domain.IssuedToken, error) {
	expiresAt := now.Add(j.ttl).Truncate(time.Second)
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(int64(userID), 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("sign token: %w", err)
	}
	return domain.IssuedToken{
		Value:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// Verify checks the validity of the given JWT token and returns the associated user ID if valid.
func (j *JWT) Verify(token string, now time.Time) (domain.UserID, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", domain.ErrInvalidToken, err)
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, domain.ErrInvalidToken
	}
	return domain.UserID(userID), nil
}
