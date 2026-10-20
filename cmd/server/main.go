package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/term"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/config"
	"github.com/if3110/tubes-db-platform/internal/store"
	"github.com/if3110/tubes-db-platform/internal/web"
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

func runServer(cfg *config.Config) {
	db := mustOpenDB(cfg.MetadataDBURL)
	defer db.Close()

	view, err := web.NewRenderer()
	if err != nil {
		log.Fatal(err)
	}
	sessions := auth.NewStore(db, auth.SessionConfig{
		IdleTimeout:      cfg.SessionIdleTimeout,
		AbsoluteTimeout:  cfg.SessionAbsoluteTimeout,
		RotationInterval: cfg.SessionRotationInterval,
		GracePeriod:      cfg.SessionGracePeriod,
		Secure:           cfg.IsProduction(),
	})
	authHandler := auth.NewHandler(db, sessions, view)
	handler := web.NewRouter(sessions, authHandler, view, cfg.IsProduction())

	log.Printf("server jalan di http://localhost:%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	log.Fatal(http.ListenAndServe(":"+cfg.AppPort, handler))
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

func mustOpenDB(url string) *pgxpool.Pool {
	db, err := store.Open(context.Background(), url)
	if err != nil {
		log.Fatalf("%v\npastikan database jalan (make db-up) dan METADATA_DB_URL benar", err)
	}
	return db
}
