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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/term"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/admin"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/config"
	"github.com/if3110/tubes-db-platform/internal/dbmgmt"
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

	cipher, err := dbmgmt.NewCipher(cfg.CredentialEncryptionKey)
	if err != nil {
		log.Fatalf("%v\nisi CREDENTIAL_ENCRYPTION_KEY di .env, bikin dengan: openssl rand -base64 32", err)
	}
	dbStore := dbmgmt.NewStore(db, cipher)
	pools := dbmgmt.NewManager(dbStore)
	defer pools.Close()

	// Adapter dari access ke tipe fungsi dbmgmt (ADR-009).
	perms := access.NewStore(db)
	isAdmin := func(ctx context.Context, userID uuid.UUID) (bool, error) {
		u := auth.CurrentUser(ctx)
		return u != nil && u.ID == userID && access.IsPlatformAdmin(ctx), nil
	}
	level := func(ctx context.Context, userID, dbID uuid.UUID) (string, error) {
		l, err := perms.EffectiveLevel(ctx, userID, dbID)
		return string(l), err
	}
	dbHandler := dbmgmt.NewHandler(dbStore, pools, view, isAdmin, level)

	adminStore := admin.NewStore(db)
	adminHandler := admin.NewHandler(db, adminStore, sessions, view)

	register := func(mux *http.ServeMux) {
		dbHandler.RegisterRoutes(mux)
		adminHandler.RegisterRoutes(mux)
	}
	handler := web.NewRouter(sessions, authHandler, register, view, cfg.IsProduction())

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
