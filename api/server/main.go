package main

import (
	"bucketd/internal/controller"
	"bucketd/internal/handler"
	"bucketd/internal/repository"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func loadEnv(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	loadEnv(".env")

	port := getEnvOrDefault("PORT", "7071")
	jwtKey := getEnvOrDefault("JWT_SECRET", "97126c91-2838-45c1-a017-8e22dac15fd0")
	storageRoot := getEnvOrDefault("STORAGE_ROOT", "/home/iamgrudge/Configs/PE/portfolio/bucketd/tests/objectd")
	dbPath := getEnvOrDefault("DB_PATH", "file:/home/iamgrudge/Configs/PE/portfolio/bucketd/tests/bucketd.db?_foreign_keys=on")

	r := repository.NewRepository(dbPath)
	if r == nil {
		log.Fatalln("Failed to connect DB")
	}

	h := handler.NewHandler(r, storageRoot)
	c := controller.NewController(h)
	mux := http.NewServeMux()

	jwtSecretBytes := []byte(jwtKey)

	// --- Bucket Management ---
	// Create Bucket
	mux.HandleFunc("PUT /{bucket}", controller.JWTAuthMiddleware(jwtSecretBytes, c.CreateBucketController))

	// List Buckets
	mux.HandleFunc("GET /{$}", controller.JWTAuthMiddleware(jwtSecretBytes, c.ListBucketsController))

	// Head Bucket (Check if bucket exists)
	mux.HandleFunc("HEAD /{bucket}", controller.JWTAuthMiddleware(jwtSecretBytes, c.ValidateBucketExistenceController))

	// Delete Bucket
	mux.HandleFunc("DELETE /{bucket}", controller.JWTAuthMiddleware(jwtSecretBytes, c.DeleteBucketController))

	// --- Object Management ---
	// Add / Upload Object
	mux.HandleFunc("PUT /{bucket}/{key...}", controller.JWTAuthMiddleware(jwtSecretBytes, c.UploadObjectController))

	// List Objects in Bucket
	mux.HandleFunc("GET /{bucket}", controller.JWTAuthMiddleware(jwtSecretBytes, c.ListObjectsController))

	// Get / Download Object
	mux.HandleFunc("GET /{bucket}/{key...}", controller.JWTAuthMiddleware(jwtSecretBytes, c.GetObjectController))

	// Head Object (Get object metadata headers)
	mux.HandleFunc("HEAD /{bucket}/{key...}", controller.JWTAuthMiddleware(jwtSecretBytes, c.GetObjectMetadataController))

	// Delete Object
	mux.HandleFunc("DELETE /{bucket}/{key...}", controller.JWTAuthMiddleware(jwtSecretBytes, c.DeleteObjectController))

	fmt.Printf("Server Listening On localhost:%s\n", port)
	http.ListenAndServe(":"+port, mux)
}
