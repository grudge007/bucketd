package controller

import (
	"bucketd/internal/handler"
	"bucketd/internal/model"
	"bucketd/internal/repository"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Controller struct {
	Handler *handler.Handler
}

type contextKey string

const userIDKey contextKey = "user_id"

var (
	ErrNoSuchBucket   = repository.ErrNoSuchBucket
	ErrAccessDenied   = repository.ErrAccessDenied
	ErrBucketNotEmpty = repository.ErrBucketNotEmpty
	ErrNoSuchKey      = repository.ErrNoSuchKey
)

func NewController(handler *handler.Handler) *Controller {
	return &Controller{
		Handler: handler,
	}
}

func formatHTTPDate(dt string) string {
	if t, err := time.Parse("2006-01-02 15:04:05", dt); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}
	if t, err := time.Parse(time.RFC3339, dt); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}
	return dt
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

func (c *Controller) CreateBucketController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	ownerId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "We encountered an internal error. Please try again.", "/"+bucketName)
		return
	}

	err := c.Handler.CreateBucketHandler(bucketName, ownerId)
	if err != nil {
		if errors.Is(err, repository.ErrBucketAlreadyExists) {
			sendS3Error(w, http.StatusConflict, "BucketAlreadyExists", "The requested bucket name already exists.", "/"+bucketName)
			return
		}
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
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

	object := model.Object{
		BucketName: bucketName,
		Key:        objectKey,
		CreatedBy:  userID,
	}
	etag, err := c.Handler.AddObjectHandler(object, r.Body)
	if err != nil {
		if errors.Is(err, repository.ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

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
		if errors.Is(err, repository.ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrNoSuchKey) || errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
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
		if errors.Is(err, repository.ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), userId)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(resp)
}

func (c *Controller) DeleteBucketController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	err := c.Handler.DeleteBucketHandler(bucketName, userId)
	if err != nil {
		if errors.Is(err, repository.ErrNoSuchBucket) || errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrBucketNotEmpty) {
			sendS3Error(w, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty.", r.URL.Path)
			return
		}

		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}
}

func (c *Controller) GetObjectController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	key := r.PathValue("key")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	objectpath, object, err := c.Handler.GetObjectHandler(bucketName, key, userId)
	if err != nil {
		if errors.Is(err, repository.ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrNoSuchKey) || errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	file, err := os.Open(objectpath)
	if err != nil {
		if os.IsNotExist(err) {
			sendS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist on disk.", r.URL.Path)
			return
		}
		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, object.Etag))
	w.Header().Set("Last-Modified", formatHTTPDate(object.LastModified))
	w.Header().Set("Accept-Ranges", "bytes")

	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		log.Printf("Error streaming file %s to client: %v", key, err)
	}
}

func (c *Controller) ValidateBucketExistenceController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	err := c.Handler.ValidateBucketExistenceHandler(bucketName, userId)
	if err != nil {
		if errors.Is(err, repository.ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", err.Error(), r.URL.Path)
			return
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", err.Error(), r.URL.Path)
			return
		}

		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (c *Controller) GetObjectMetadataController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	key := r.PathValue("key")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	object, err := c.Handler.GetObjectMetadataHandler(bucketName, key, userId)
	if err != nil {
		if errors.Is(err, repository.ErrAccessDenied) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if errors.Is(err, repository.ErrNoSuchBucket) || errors.Is(err, repository.ErrNoSuchKey) || errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, object.Etag))
	w.Header().Set("Last-Modified", formatHTTPDate(object.LastModified))

	w.WriteHeader(http.StatusOK)
}
