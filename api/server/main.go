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
	root   = "/tmp/bucketd"
)

func main() {
	r := repository.NewRepository()
	if r == nil {
		log.Fatalln("Failed to connect DB")
	}

	h := handler.NewHandler(r, root)
	c := controller.NewController(h)
	mux := http.NewServeMux()

	mux.HandleFunc("POST /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.CreateBucketController))
	fmt.Println("Server Listening On localhost:7071")
	http.ListenAndServe(":7071", mux)
}
