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

	mux.HandleFunc("PUT /{bucket}", controller.JWTAuthMiddleware([]byte(jwtKey), c.CreateBucketController))
	mux.HandleFunc("PUT /{bucket}/{key...}", controller.JWTAuthMiddleware([]byte(jwtKey), c.UploadObjectController))

	fmt.Println("Server Listening On localhost:7071")
	http.ListenAndServe(":7071", mux)
}
