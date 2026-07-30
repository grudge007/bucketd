package main

import (
	"bucketd/internal/controller"
	"bucketd/internal/handler"
	"bucketd/internal/repository"
	"fmt"
	"log"
	"net/http"
)

const (
	jwtKey = "97126c91-2838-45c1-a017-8e22dac15fd0"
	root   = "/home/iamgrudge/Configs/PE/portfolio/bucketd/tests/objectd"
)

func main() {
	r := repository.NewRepository()
	if r == nil {
		log.Fatalln("Failed to connect DB")
	}

	h := handler.NewHandler(r, root)
	c := controller.NewController(h)
	mux := http.NewServeMux()

	// --- Bucket Management ---
	// Create Bucket
	mux.HandleFunc("PUT /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.CreateBucketController))

	// List Buckets
	mux.HandleFunc("GET /", controller.JWTAuthMiddleware([]byte(jwtKey), c.ListBucketsController))

	// Head Bucket (Check if bucket exists)
	mux.HandleFunc("HEAD /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.ValidateBucketExistenceController))

	// Delete Bucket
	mux.HandleFunc("DELETE /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.DeleteBucketController))

	// --- Object Management ---
	// Add / Upload Object
	mux.HandleFunc("PUT /{bucket}/{key...}", controller.JWTAuthMiddleware([]byte(jwtKey), c.UploadObjectController))

	// List Objects in Bucket
	mux.HandleFunc("GET /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.ListObjectsController))

	// Get / Download Object
	mux.HandleFunc("GET /{bucket}/{key...}", controller.JWTAuthMiddleware([]byte(jwtKey), c.GetObjectController))

	// Head Object (Get object metadata headers)
	mux.HandleFunc("HEAD /{bucket}/{key...}", controller.JWTAuthMiddleware([]byte(jwtKey), c.GetObjectMetadataController))

	// Delete Object
	mux.HandleFunc("DELETE /{bucket}/{key...}", controller.JWTAuthMiddleware([]byte(jwtKey), c.DeleteObjectController))

	fmt.Println("Server Listening On localhost:7071")
	http.ListenAndServe(":7071", mux)
}
