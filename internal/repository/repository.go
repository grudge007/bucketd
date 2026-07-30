package repository

import (
	"bucketd/internal/model"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Repository struct {
	DB *sql.DB
}

var (
	ErrNoSuchBucket        = errors.New("the specified bucket does not exist")
	ErrAccessDenied        = errors.New("access denied")
	ErrBucketNotEmpty      = errors.New("the specified bucket is not empty")
	ErrNoSuchKey           = errors.New("the specified key does not exist")
	ErrBucketAlreadyExists = errors.New("the requested bucket name already exists")
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
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrBucketAlreadyExists
		}
		return err
	}

	return nil
}

func (d *Repository) InsertObject(object model.Object) error {
	if object.StorageClass == "" {
		object.StorageClass = "STANDARD"
	}
	query := `INSERT INTO objects (bucket_name, key, size, etag, storage_class, created_by, last_modified) 
	VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(bucket_name, key) DO UPDATE SET 
		size=excluded.size, 
		etag=excluded.etag, 
		storage_class=excluded.storage_class, 
		created_by=excluded.created_by, 
		last_modified=CURRENT_TIMESTAMP`
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
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNoSuchKey // Object didn't exist
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
		return nil, fmt.Errorf("querying objects failed: %w", err)
	}
	defer rows.Close()

	var objects []model.Content

	for rows.Next() {
		var object model.Content

		err := rows.Scan(&object.Key, &object.ETag, &object.Size, &object.StorageClass, &object.LastModified)
		if err != nil {
			return nil, fmt.Errorf("scanning object row failed: %w", err)
		}

		objects = append(objects, object)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return objects, nil
}

func (r *Repository) ValidateBucketEmpty(bucketName string) (bool, error) {
	// Query returns 1 if at least one object exists, 0 if empty
	query := `SELECT EXISTS(SELECT 1 FROM objects WHERE bucket_name = ?)`

	var exists bool
	err := r.DB.QueryRow(query, bucketName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if bucket is empty: %w", err)
	}

	// If exists is true, the bucket is NOT empty
	isEmpty := !exists
	return isEmpty, nil
}

func (r *Repository) DeleteBucket(bucketName string) error {
	query := `DELETE FROM buckets WHERE name = ?`
	_, err := r.DB.Exec(query, bucketName)
	return err
}

func (r *Repository) GetObjectRecord(bucketName, key string) (*model.Object, error) {
	query := `
        SELECT bucket_name, key, size, etag, storage_class, created_by, last_modified 
        FROM objects 
        WHERE bucket_name = ? AND key = ?
    `

	var obj model.Object
	err := r.DB.QueryRow(query, bucketName, key).Scan(
		&obj.BucketName,
		&obj.Key,
		&obj.Size,
		&obj.Etag,
		&obj.StorageClass,
		&obj.CreatedBy,
		&obj.LastModified,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSuchKey
		}
		return nil, err
	}

	return &obj, nil
}

func (r *Repository) GetObjectMetadata(bucketName, key string) (*model.Object, error) {
	query := `
        SELECT key, size, etag, storage_class, last_modified 
        FROM objects 
        WHERE bucket_name = ? AND key = ?
    `

	var obj model.Object
	err := r.DB.QueryRow(query, bucketName, key).Scan(
		&obj.Key,
		&obj.Size,
		&obj.Etag,
		&obj.StorageClass,
		&obj.LastModified,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSuchKey
		}
		return nil, err
	}

	return &obj, nil
}
