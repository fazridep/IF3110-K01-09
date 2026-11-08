package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/dbmgmt"
	"github.com/if3110/tubes-db-platform/internal/migration"
	"github.com/if3110/tubes-db-platform/internal/platform/config"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/platform/store"
	"github.com/if3110/tubes-db-platform/internal/queue"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	level, err := obs.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("OTEL_SERVICE_NAME") == "" {
		cfg.ServiceName = "db-platform-worker"
	}
	if err := obs.Setup(context.Background(), obs.Config{Service: cfg.ServiceName, Level: level, TraceExporter: cfg.TraceExporter}); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.MetadataDBURL)
	if err != nil {
		log.Fatalf("%v\npastikan database jalan (make db-up) dan METADATA_DB_URL benar", err)
	}
	defer db.Close()

	cipher, err := dbmgmt.NewCipher(cfg.CredentialEncryptionKey)
	if err != nil {
		log.Fatalf("%v\nisi CREDENTIAL_ENCRYPTION_KEY di .env, samakan dengan server", err)
	}
	pools := dbmgmt.NewManager(dbmgmt.NewStore(db, cipher))
	defer pools.Close()

	executor := migration.NewExecutor(migration.NewService(db, access.NewStore(db), pools))
	worker := queue.NewWorker(queue.NewStore(db), executor, queue.WorkerConfig{
		Concurrency:  cfg.WorkerConcurrency,
		PollInterval: cfg.WorkerPollInterval,
		LeaseTTL:     cfg.WorkerLeaseTTL,
	})
	worker.Run(ctx)
}
