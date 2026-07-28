package repository

import (
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
