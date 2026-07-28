package handler

import (
	"bucketd/internal/model"
	"bucketd/internal/repository"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

type Handler struct {
	Repository *repository.Repository
	Root       string
	db         *sql.DB
}

const root = "/tmp/bucketd"

func NewHandler(repository *repository.Repository, root string) *Handler {
	return &Handler{
		Repository: repository,
		Root:       root,
		db:         repository.DB,
	}
}

func (h *Handler) CreateBucketHandler(bucketName, ownerId string) error {
	err := h.Repository.InsertBucket(bucketName, ownerId)
	if err != nil {
		log.Println("failed to insert data into bucket table: ", err)
		return fmt.Errorf("failed to create bucket: %v", err)
	}

	err = os.MkdirAll(filepath.Join(root, bucketName), 0666)
	if err != nil {
		log.Println("failed to create dir: ", err)
		return fmt.Errorf("failed to create bucket: %v", err)
	}
	return nil
}

func (h *Handler) AddObjectToBucketHandler(object model.Object, body io.Reader) (string, error) {
	filePath := filepath.Join(h.Root, object.BucketName, object.Key)

	// 1. Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0766); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	// 2. Create the file on disk
	outFile, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file on disk: %w", err)
	}
	defer outFile.Close()

	// 3. Initialize MD5 hasher
	hasher := md5.New()

	// 4. Combine disk writer and hasher writer
	// Anything written to 'mw' is written to BOTH outFile and hasher
	mw := io.MultiWriter(outFile, hasher)

	// 5. Stream from body -> multi-writer in small chunks (never fills RAM)
	object.Size, err = io.Copy(mw, body)
	if err != nil {
		return "", fmt.Errorf("failed to write object content to disk: %w", err)
	}

	// 6. Compute ETag (hex string)
	object.Etag = hex.EncodeToString(hasher.Sum(nil))

	err = h.Repository.InsertObject(object)
	if err != nil {
		return "", fmt.Errorf("failed to add object: %v", err)
	}

	return object.Etag, nil
}
