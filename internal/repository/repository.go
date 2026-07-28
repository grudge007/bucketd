package repository

import (
	"bucketd/internal/model"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type Repository struct {
	DB *sql.DB
}

func NewRepository() *Repository {
	db, err := sql.Open("sqlite3", "file:/home/iamgrudge/Configs/PE/portfolio/bucketd/tests/bucketd.db?_foreign_keys=on")
	if err != nil {
		fmt.Println(err)
		log.Fatal("Error: DB Connection Failed")
	}

	db.SetMaxOpenConns(1)

	return &Repository{
		DB: db,
	}
}

func (d *Repository) InsertBucket(name, owner string) error {
	query := `INSERT INTO buckets (name, owner_id) VALUES (?, ?)`
	_, err := d.DB.Exec(query, name, owner)
	if err != nil {
		return err
	}

	return nil
}

func (d *Repository) InsertObject(object model.Object) error {
	if object.StorageClass == "" {
		object.StorageClass = "STANDARD"
	}
	query := `INSERT INTO objects (bucket_name, key, size, etag, storage_class, created_by) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := d.DB.Exec(query, object.BucketName, object.Key, object.Size, object.Etag, object.StorageClass, object.CreatedBy)
	if err != nil {
		return err
	}

	return nil
}
