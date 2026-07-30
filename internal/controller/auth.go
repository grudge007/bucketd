package controller

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

func JWTAuthMiddleware(jwtSecret []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Missing or invalid authorization header.", r.URL.Path)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
			// Check algorithm name directly
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			log.Println("failed here: ", err)
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Missing or invalid authorization header.", r.URL.Path)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Missing or invalid authorization header.", r.URL.Path)
			return
		}

		// 1. Extract using "UserID" key
		val, ok := claims[string(userIDKey)]
		if !ok {
			// log.Println("failed here: ", err)
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Missing or invalid authorization header.", r.URL.Path)
			return
		}

		userID, ok := val.(string)
		if !ok || userID == "" {
			log.Println("failed here: ", err)
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Missing or invalid authorization header.", r.URL.Path)
			return
		}

		// 2. Attach using typed context key (userIDKey), NOT string variable
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}
