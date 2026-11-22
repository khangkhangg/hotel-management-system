package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"hotelpms/api/internal/config"
	"hotelpms/api/internal/httpx"
)

func main() {
	if _, err := config.Load(); err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: httpx.NewRouter(httpx.Deps{}), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
