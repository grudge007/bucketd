package repository

import (
	"bucketd/internal/model"
	"database/sql"
	"errors"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type Repository struct {
	DB *sql.DB
}

var (
	ErrNoSuchBucket = errors.New("the specified bucket does not exist")
	ErrAccessDenied = errors.New("access denied")
)

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

func (r *Repository) ValidateUserAgainstBucket(bucketName, userId string) error {
	query := `SELECT owner_id FROM buckets WHERE name = ?`

	var ownerID string
	err := r.DB.QueryRow(query, bucketName).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Bucket does not exist in S3 -> Return NoSuchBucket error
			return ErrNoSuchBucket
		}
		return fmt.Errorf("database query failed: %w", err)
	}

	// Check if the authenticated user owns this bucket
	if ownerID != userId {
		return ErrAccessDenied
	}

	return nil
}

func (r *Repository) DeleteObject(bucketName, key string) error {
	query := `DELETE FROM objects WHERE bucket_name = ? AND key = ?`
	res, err := r.DB.Exec(query, bucketName, key)
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows // Object didn't exist
	}

	return nil
}

func (r *Repository) ListBuckets(ownerId string) ([]model.Bucket, error) {
	query := `SELECT name, created_at FROM buckets WHERE owner_id = ?`

	rows, err := r.DB.Query(query, ownerId)
	if err != nil {
		return nil, fmt.Errorf("querying buckets failed: %w", err)
	}
	defer rows.Close()

	var buckets []model.Bucket

	// Iterate through result set
	for rows.Next() {
		var bucket model.Bucket

		// 1. MUST pass memory addresses using '&'
		err := rows.Scan(&bucket.Name, &bucket.CreationDate)
		if err != nil {
			return nil, fmt.Errorf("scanning bucket row failed: %w", err)
		}

		buckets = append(buckets, bucket)
	}

	// 2. Always check rows.Err() for iteration errors
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return buckets, nil
}

func (r *Repository) ListObjects(ownerId, bucketName string) ([]model.Content, error) {
	query := `SELECT key, etag, size, storage_class, last_modified FROM objects WHERE created_by = ? AND bucket_name = ?`

	rows, err := r.DB.Query(query, ownerId, bucketName)
	if err != nil {
		return nil, fmt.Errorf("querying buckets failed: %w", err)
	}
	defer rows.Close()

	var objects []model.Content

	for rows.Next() {
		var object model.Content

		err := rows.Scan(&object.Key, &object.ETag, &object.Size, &object.StorageClass, &object.LastModified)
		if err != nil {
			return nil, fmt.Errorf("scanning bucket row failed: %w", err)
		}

		objects = append(objects, object)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return objects, nil
}
