package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	listenString := ":8080"

	if port := os.Getenv("PORT"); port != "" {
		listenString = ":" + port
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, you've requested: %s\n", r.URL.Path)
	})
	_ = http.ListenAndServe(listenString, nil)
}
