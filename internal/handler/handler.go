package handler

import (
	"bucketd/internal/model"
	"bucketd/internal/repository"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrBucketNotEmpty = repository.ErrBucketNotEmpty
	ErrNoSuchBucket   = repository.ErrNoSuchBucket
	ErrAccessDenied   = repository.ErrAccessDenied
	ErrNoSuchKey      = repository.ErrNoSuchKey
)

type Handler struct {
	Repository *repository.Repository
	Root       string
	db         *sql.DB
	MaxKeys    int
}

func NewHandler(repository *repository.Repository, root string) *Handler {
	return &Handler{
		Repository: repository,
		Root:       root,
		db:         repository.DB,
		MaxKeys:    100,
	}
}

func (h *Handler) getSafeObjectPath(bucketName, key string) (string, error) {
	bucketDir := filepath.Clean(filepath.Join(h.Root, bucketName))
	filePath := filepath.Clean(filepath.Join(bucketDir, key))

	rel, err := filepath.Rel(bucketDir, filePath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("invalid object key path")
	}

	return filePath, nil
}

func (h *Handler) CreateBucketHandler(bucketName, ownerId string) error {
	err := h.Repository.InsertBucket(bucketName, ownerId)
	if err != nil {
		log.Println("failed to insert data into bucket table: ", err)
		return err
	}

	err = os.MkdirAll(filepath.Join(h.Root, bucketName), 0755)
	if err != nil {
		_ = h.Repository.DeleteBucket(bucketName) // Roll back DB insert on disk creation failure
		log.Println("failed to create dir: ", err)
		return fmt.Errorf("failed to create bucket directory: %v", err)
	}
	return nil
}

func (h *Handler) AddObjectHandler(object model.Object, body io.Reader) (string, error) {
	// 1. Validate bucket existence and user authorization
	if err := h.Repository.ValidateUserAgainstBucket(object.BucketName, object.CreatedBy); err != nil {
		return "", err
	}

	// 2. Validate object path for directory traversal
	filePath, err := h.getSafeObjectPath(object.BucketName, object.Key)
	if err != nil {
		return "", err
	}

	filePathTmp := fmt.Sprintf("%s.tmp.%d", filePath, time.Now().UnixNano())

	// 3. Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(filePathTmp), 0755); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	// 4. Create the temp file on disk
	outFile, err := os.Create(filePathTmp)
	if err != nil {
		return "", fmt.Errorf("failed to create file on disk: %w", err)
	}

	// Cleanup hook: Runs on function exit.
	defer func() {
		outFile.Close()
		os.Remove(filePathTmp)
	}()

	// 5. Initialize MD5 hasher & MultiWriter
	hasher := md5.New()
	mw := io.MultiWriter(outFile, hasher)

	// 6. Stream body to temp file and hasher simultaneously
	object.Size, err = io.Copy(mw, body)
	if err != nil {
		return "", fmt.Errorf("failed to write object content to disk: %w", err)
	}

	// 7. Compute ETag (hex string)
	object.Etag = hex.EncodeToString(hasher.Sum(nil))

	// 8. Explicitly close file before DB insert & rename
	if err := outFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// 9. Insert metadata into DB
	err = h.Repository.InsertObject(object)
	if err != nil {
		return "", fmt.Errorf("failed to add object metadata to DB: %w", err)
	}

	// 10. Atomic Rename
	if err := os.Rename(filePathTmp, filePath); err != nil {
		return "", fmt.Errorf("failed to rename temp object file: %w", err)
	}

	return object.Etag, nil
}

func (h *Handler) DeleteObjectHandler(bucketName, key, userId string) error {
	err := h.Repository.ValidateUserAgainstBucket(bucketName, userId)
	if err != nil {
		return err
	}

	err = h.Repository.DeleteObject(bucketName, key)
	if err != nil {
		return err
	}

	objectpath, err := h.getSafeObjectPath(bucketName, key)
	if err == nil {
		if err := os.Remove(objectpath); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove file on disk: %v", err)
		}
	}

	return nil
}

func (h *Handler) ListBucketsHandler(ownerId string) (model.ListAllMyBucketsResult, error) {
	buckets, err := h.Repository.ListBuckets(ownerId)
	if err != nil {
		return model.ListAllMyBucketsResult{}, err
	}

	if buckets == nil {
		buckets = []model.Bucket{}
	}

	resp := model.ListAllMyBucketsResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: model.Owner{
			ID:          ownerId,
			DisplayName: ownerId,
		},
		Buckets: model.Buckets{
			Buckets: buckets,
		},
	}

	return resp, nil
}

func (h *Handler) ListObjectsHandler(ownerId, bucketName string) (model.ListObjectsResult, error) {
	if err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId); err != nil {
		return model.ListObjectsResult{}, err
	}

	objects, err := h.Repository.ListObjects(ownerId, bucketName)
	if err != nil {
		return model.ListObjectsResult{}, err
	}

	if objects == nil {
		objects = []model.Content{}
	}

	resp := model.ListObjectsResult{
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucketName,
		Prefix:      "",
		KeyCount:    len(objects),
		MaxKeys:     h.MaxKeys,
		IsTruncated: false,
		Contents:    objects,
	}
	return resp, nil
}

func (h *Handler) DeleteBucketHandler(bucketName, ownerId string) error {
	if err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId); err != nil {
		return err
	}

	isEmpty, err := h.Repository.ValidateBucketEmpty(bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket is empty: %w", err)
	}

	if !isEmpty {
		return repository.ErrBucketNotEmpty
	}

	if err := h.Repository.DeleteBucket(bucketName); err != nil {
		return fmt.Errorf("failed to delete bucket record: %w", err)
	}

	bucketPath := filepath.Join(h.Root, bucketName)
	if err := os.Remove(bucketPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to delete bucket directory %s: %v", bucketPath, err)
	}

	return nil
}

func (h *Handler) GetObjectHandler(bucketName, key, userID string) (string, *model.Object, error) {
	// A. Validate user permissions for the bucket
	if err := h.Repository.ValidateUserAgainstBucket(bucketName, userID); err != nil {
		return "", nil, err
	}

	// B. Validate object path for directory traversal
	filePath, err := h.getSafeObjectPath(bucketName, key)
	if err != nil {
		return "", nil, repository.ErrNoSuchKey
	}

	// C. Get metadata from database
	obj, err := h.Repository.GetObjectRecord(bucketName, key)
	if err != nil {
		return "", nil, err
	}

	return filePath, obj, nil
}

func (h *Handler) ValidateBucketExistenceHandler(bucketName, ownerId string) error {
	err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNoSuchBucket
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			return repository.ErrAccessDenied
		}
		return err
	}

	return nil
}

func (h *Handler) GetObjectMetadataHandler(bucketName, key, ownerId string) (*model.Object, error) {
	err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNoSuchBucket
		}
		if errors.Is(err, repository.ErrAccessDenied) {
			return nil, repository.ErrAccessDenied
		}
		return nil, err
	}

	object, err := h.Repository.GetObjectMetadata(bucketName, key)
	if err != nil {
		return nil, err
	}

	return object, nil
}
