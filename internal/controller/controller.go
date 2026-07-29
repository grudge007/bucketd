package controller

import (
	"bucketd/internal/handler"
	"bucketd/internal/model"
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Controller struct {
	Handler *handler.Handler
}

type contextKey string

const userIDKey contextKey = "user_id"

var (
	ErrNoSuchBucket = errors.New("bucket does not exist")
	ErrAccessDenied = errors.New("access denied")
)

func NewController(handler *handler.Handler) *Controller {
	return &Controller{
		Handler: handler,
	}
}

func (c *Controller) CreateBucketController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	ownerId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "We encountered an internal error. Please try again.", "/"+bucketName)
		return
	}

	err := c.Handler.CreateBucketHandler(bucketName, ownerId)
	if err != nil {
		sendS3Error(w, http.StatusConflict, "BucketAlreadyExists", "The requested bucket name already exists.", "/"+bucketName)
		return

	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Location", "/"+bucketName)
	w.WriteHeader(http.StatusOK)

}

func (c *Controller) UploadObjectController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	objectKey := r.PathValue("key")

	userID, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	if bucketName == "" || objectKey == "" {
		sendS3Error(w, http.StatusBadRequest, "InvalidArgument", "Bucket and Key are required", r.URL.Path)
		return
	}

	// Stream r.Body directly to storage and database
	object := model.Object{
		BucketName: bucketName,
		Key:        objectKey,
		CreatedBy:  userID,
	}
	etag, err := c.Handler.AddObjectHandler(object, r.Body)
	if err != nil {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	// S3 standard headers for successful object upload
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, etag))
	w.WriteHeader(http.StatusOK)
}

func (c *Controller) DeleteObjectController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	key := r.PathValue("key")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	err := c.Handler.DeleteObjectHandler(bucketName, key, userId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist", r.URL.Path)
			return
		}
		if errors.Is(err, ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}

		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (c *Controller) ListBucketsController(w http.ResponseWriter, r *http.Request) {
	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	resp, err := c.Handler.ListBucketsHandler(userId)
	if err != nil {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), userId)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(resp)
}

func (c *Controller) ListObjectsController(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("list-type") != "2" {
		sendS3Error(w, http.StatusBadRequest, "InvalidArgument", "Unsupported list-type parameter", r.URL.Path)
		return
	}

	bucketName := r.PathValue("bucket")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	resp, err := c.Handler.ListObjectsHandler(userId, bucketName)
	if err != nil {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), userId)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(resp)
}

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

func sendS3Error(w http.ResponseWriter, status int, code, message, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(model.S3Error{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestId: "tx000000000000000000001",
	})
}
