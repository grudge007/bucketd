package controller

import (
	"bucketd/internal/handler"
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Controller struct {
	Handler *handler.Handler
}

type contextKey string

const userIDKey contextKey = "userID"

func NewController(handler *handler.Handler) *Controller {
	return &Controller{
		Handler: handler,
	}
}

func (c *Controller) CreateBucketController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	ownerId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		http.Error(w, "Internal error: missing user context", http.StatusInternalServerError)
		return
	}

	err := c.Handler.CreateBucketHandler(bucketName, ownerId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

func JWTAuthMiddleware(jwtSecret []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			// sendS3Error(w, "AccessDenied", "Missing or invalid authorization header", http.StatusForbidden)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		// Parse & Validate JWT signature using shared secret/public key
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			// sendS3Error(w, "AccessDenied", "Invalid token signature or expired", http.StatusForbidden)
			return
		}

		// Extract claims (e.g., user_id)
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			// sendS3Error(w, "AccessDenied", "Invalid token claims", http.StatusForbidden)
			return
		}

		userID := claims["sub"].(string)

		// Attach user ID to context and proceed
		ctx := context.WithValue(r.Context(), "user_id", userID)
		next(w, r.WithContext(ctx))
	}
}
