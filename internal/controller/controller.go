package controller

import (
	"bucketd/internal/handler"
	"bucketd/internal/model"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
)

type Controller struct {
	Handler *handler.Handler
}

type contextKey string

const userIDKey contextKey = "user_id"

var (
	ErrNoSuchBucket   = errors.New("bucket does not exist")
	ErrAccessDenied   = errors.New("access denied")
	ErrBucketNotEmpty = errors.New("the specified bucket is not empty")
	ErrNoSuchKey      = errors.New("the specified key does not exist")
)

func NewController(handler *handler.Handler) *Controller {
	return &Controller{
		Handler: handler,
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

func (c *Controller) DeleteBucketController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	err := c.Handler.DeleteBucketHandler(bucketName, userId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", r.URL.Path)
			return
		}
		if errors.Is(err, ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied", r.URL.Path)
			return
		}
		if errors.Is(err, ErrBucketNotEmpty) {
			sendS3Error(w, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty.", r.URL.Path)
			return
		}

		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}
}

func (c *Controller) GetObjectController(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("")

	key := r.PathValue("key")

	userId, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		sendS3Error(w, http.StatusInternalServerError, "InternalError", "Missing user context", r.URL.Path)
		return
	}

	objectpath, object, err := c.Handler.GetObjectHandler(bucketName, key, userId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", r.URL.Path)
			return
		}
		if errors.Is(err, ErrAccessDenied) {
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
	w.Header().Set("Last-Modified", object.LastModified)
	w.Header().Set("Accept-Ranges", "bytes")

	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		// Log connection breaks (e.g. client canceled download early)
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
		if errors.Is(err, ErrNoSuchBucket) {
			sendS3Error(w, http.StatusNotFound, "NoSuchBucket", err.Error(), r.URL.Path)
			return
		}
		if errors.Is(err, ErrAccessDenied) {
			sendS3Error(w, http.StatusForbidden, "AccessDenied", err.Error(), r.URL.Path)
			return
		}

		sendS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	// Success response for HEAD / Bucket check -> 200 OK with no body
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
		if errors.Is(err, ErrAccessDenied) {
			w.WriteHeader(http.StatusForbidden) // 403 (No body for HEAD)
			return
		}

		if errors.Is(err, ErrNoSuchBucket) {
			w.WriteHeader(http.StatusNotFound) // 404 (No body for HEAD)
			return
		}

		w.WriteHeader(http.StatusInternalServerError) // 500
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, object.Etag))
	w.Header().Set("Last-Modified", object.LastModified)

	w.WriteHeader(http.StatusOK)
}
