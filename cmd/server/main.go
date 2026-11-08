// Command server menjalankan platform. Tanpa argumen jalanin HTTP server
// (server.go); "migrate" dan "seed-admin" buat setup database metadata.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/term"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/config"
	"github.com/if3110/tubes-db-platform/internal/platform/store"
	"github.com/if3110/tubes-db-platform/migrations"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		runServer(cfg)
	case "migrate":
		runMigrate(cfg)
	case "seed-admin":
		runSeedAdmin(cfg)
	default:
		fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %s\npakai: migrate | seed-admin | (tanpa argumen untuk jalanin server)\n", cmd)
		os.Exit(1)
	}
}

func runMigrate(cfg *config.Config) {
	db := mustOpenDB(cfg.MetadataDBURL)
	defer db.Close()

	applied, err := store.Migrate(context.Background(), db, migrations.Metadata)
	for _, v := range applied {
		log.Printf("migrate: %s", v)
	}
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if len(applied) == 0 {
		log.Print("migrate: sudah paling baru")
	}
}

func runSeedAdmin(cfg *config.Config) {
	db := mustOpenDB(cfg.MetadataDBURL)
	defer db.Close()

	fmt.Print("Username admin: ")
	username, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	username = strings.TrimSpace(username)
	fmt.Print("Password: ")
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		log.Fatalf("seed-admin: baca password: %v", err)
	}

	id, err := auth.SeedAdmin(context.Background(), db, username, string(password))
	if err != nil {
		log.Fatalf("seed-admin: %v", err)
	}
	log.Printf("seed-admin: admin %s dibuat (id %s)", username, id)
}

func mustOpenDB(url string, configure ...func(*pgxpool.Config)) *pgxpool.Pool {
	db, err := store.Open(context.Background(), url, configure...)
	if err != nil {
		log.Fatalf("%v\npastikan database jalan (make db-up) dan METADATA_DB_URL benar", err)
	}
	return db
}
