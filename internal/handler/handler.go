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
	"time"
)

var (
	ErrBucketNotEmpty = errors.New("the specified bucket is not empty")
	ErrNoSuchBucket   = errors.New("the specified bucket does not exist")
	ErrAccessDenied   = errors.New("access denied")
	ErrNoSuchKey      = errors.New("the specified key does not exist")
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

func (h *Handler) CreateBucketHandler(bucketName, ownerId string) error {
	err := h.Repository.InsertBucket(bucketName, ownerId)
	if err != nil {
		log.Println("failed to insert data into bucket table: ", err)
		return fmt.Errorf("failed to create bucket: %v", err)
	}

	err = os.MkdirAll(filepath.Join(h.Root, bucketName), 0755)
	if err != nil {
		log.Println("failed to create dir: ", err)
		return fmt.Errorf("failed to create bucket: %v", err)
	}
	return nil
}

func (h *Handler) AddObjectHandler(object model.Object, body io.Reader) (string, error) {
	filePath := filepath.Join(h.Root, object.BucketName, object.Key)
	filePathTmp := fmt.Sprintf("%s.tmp.%d", filePath, time.Now().UnixNano())

	// 1. Ensure parent directories exist (0755 gives proper execution/traversal permissions)
	if err := os.MkdirAll(filepath.Dir(filePathTmp), 0755); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	// 2. Create the temp file on disk
	outFile, err := os.Create(filePathTmp)
	if err != nil {
		return "", fmt.Errorf("failed to create file on disk: %w", err)
	}

	// Cleanup hook: Runs on function exit.
	// If the file was already closed or renamed, os.Remove safely returns an error we ignore.
	defer func() {
		outFile.Close()
		os.Remove(filePathTmp) // Deletes tmp file if an error returned before os.Rename
	}()

	// 3. Initialize MD5 hasher & MultiWriter
	hasher := md5.New()
	mw := io.MultiWriter(outFile, hasher)

	// 4. Stream body to temp file and hasher simultaneously
	object.Size, err = io.Copy(mw, body)
	if err != nil {
		return "", fmt.Errorf("failed to write object content to disk: %w", err)
	}

	// 5. Compute ETag (hex string)
	object.Etag = hex.EncodeToString(hasher.Sum(nil))

	// 6. Explicitly close file before DB insert & rename so file handles are released
	if err := outFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// 7. Insert metadata into DB
	err = h.Repository.InsertObject(object)
	if err != nil {
		return "", fmt.Errorf("failed to add object metadata to DB: %w", err)
	}

	// 8. Atomic Rename: Only overwrites the real path if streaming AND DB insertion succeed!
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
		return fmt.Errorf("failed to delete object metadata: %w", err)
	}

	objectpath := filepath.Join(h.Root, bucketName, key)
	if err := os.Remove(objectpath); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove file on disk: %v", err)
	}

	return nil
}

func (h *Handler) ListBucketsHandler(ownerId string) (model.ListAllMyBucketsResult, error) {
	buckets, err := h.Repository.ListBuckets(ownerId)
	if err != nil {
		return model.ListAllMyBucketsResult{}, err
	}

	// Ensure buckets is an empty slice instead of nil so XML outputs <Buckets></Buckets> rather than omitting it
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
	objects, err := h.Repository.ListObjects(ownerId, bucketName)
	if err != nil {
		return model.ListObjectsResult{}, err
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
		return ErrBucketNotEmpty
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

	// B. Get metadata from database
	obj, err := h.Repository.GetObjectRecord(bucketName, key)
	if err != nil {
		return "", nil, err
	}

	// C. Resolve file path on disk
	filePath := filepath.Join(h.Root, bucketName, key)

	return filePath, obj, nil
}

func (h *Handler) ValidateBucketExistenceHandler(bucketName, ownerId string) error {
	err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Bucket doesn't exist in DB
			return ErrNoSuchBucket
		}
		if errors.Is(err, ErrAccessDenied) {
			// Bucket exists, but belongs to another user
			return ErrAccessDenied
		}
		return fmt.Errorf("database validation error: %w", err)
	}

	return nil
}

func (h *Handler) GetObjectMetadataHandler(bucketName, key, ownerId string) (*model.Object, error) {
	err := h.Repository.ValidateUserAgainstBucket(bucketName, ownerId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Bucket doesn't exist in DB
			return nil, ErrNoSuchBucket
		}
		if errors.Is(err, ErrAccessDenied) {
			// Bucket exists, but belongs to another user
			return nil, ErrAccessDenied
		}
		return nil, fmt.Errorf("database validation error: %w", err)
	}

	object, err := h.Repository.GetObjectMetadata(bucketName, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSuchKey
		}

		return nil, err
	}

	return object, nil
}
