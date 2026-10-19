package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			log.Fatal("belum diimplementasi: migration untuk database metadata platform")
		case "seed-admin":
			log.Fatal("belum diimplementasi: bikin akun Platform Admin pertama")
		default:
			fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %s\npakai: migrate | seed-admin | (tanpa argumen untuk jalanin server)\n", os.Args[1])
			os.Exit(1)
		}
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	// Sementara cuma halaman penanda. Routing asli masuk di internal/web.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "Database Management Platform - kerangka awal, belum ada fitur.")
	})

	log.Printf("server jalan di http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
