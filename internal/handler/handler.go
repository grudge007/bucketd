package handler

import (
	"bucketd/internal/repository"
	"database/sql"
	"fmt"
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
		return fmt.Errorf("failed to create bucket")
	}

	err = os.MkdirAll(filepath.Join(root, bucketName), 0666)
	if err != nil {
		log.Println("failed to create dir: ", err)
		return fmt.Errorf("failed to create bucket")
	}
	return nil
}
