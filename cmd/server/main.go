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
	"github.com/if3110/tubes-db-platform/internal/web"
)

func main() {
	_ = godotenv.Load()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			runMigrate()
			return
		case "seed-admin":
			runSeedAdmin()
			return
		default:
			fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %s\npakai: migrate | seed-admin | (tanpa argumen untuk jalanin server)\n", os.Args[1])
			os.Exit(1)
		}
	}
	runServer()
}

func runServer() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db := mustOpenDB(cfg.MetadataDBURL)
	defer db.Close()

	sessionStore := auth.NewStore(db, auth.SessionConfig{
		IdleTimeout:      cfg.SessionIdleTimeout,
		AbsoluteTimeout:  cfg.SessionAbsoluteTimeout,
		RotationInterval: cfg.SessionRotationInterval,
		GracePeriod:      cfg.SessionGracePeriod,
		Secure:           cfg.IsProduction(),
	})

	authHandler := auth.NewHandler(db, sessionStore)
	handler := web.NewRouter(sessionStore, authHandler)

	log.Printf("server jalan di http://localhost:%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	if err := http.ListenAndServe(":"+cfg.AppPort, handler); err != nil {
		log.Fatal(err)
	}
}

func runMigrate() {
	log.Fatal("belum diimplementasi: jalankan migration metadata (tabel users, sessions, dst)")
	// TODO: runner migration di sini
}

func runSeedAdmin() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db := mustOpenDB(cfg.MetadataDBURL)
	defer db.Close()
	
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Username admin: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	fmt.Print("Password (ketikan dihide): ")
	rawPassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		log.Fatalf("gagal membaca password: %v", err)
	}
	password := strings.TrimSpace(string(rawPassword))
	ctx := context.Background()
	if err = auth.SeedAdmin(ctx, db, username, password); err != nil {
		log.Fatalf("seed-admin gagal: %v", err)
	}
}

func mustOpenDB(url string) *pgxpool.Pool {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("db: gagal membuat pool: %v", err)
	}
	if err = pool.Ping(ctx); err != nil {
		log.Fatalf("db: gagal ping database: %v\npastikan database berjalan dan METADATA_DB_URL benar.", err)
	}
	return pool
}