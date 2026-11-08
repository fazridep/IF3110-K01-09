package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/admin"
	"github.com/if3110/tubes-db-platform/internal/approval"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/dbmgmt"
	"github.com/if3110/tubes-db-platform/internal/migration"
	"github.com/if3110/tubes-db-platform/internal/platform/config"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/queue"
	"github.com/if3110/tubes-db-platform/internal/web"
)

func runServer(cfg *config.Config) {
	level, err := obs.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	if err := obs.Setup(context.Background(), obs.Config{Service: cfg.ServiceName, Level: level, TraceExporter: cfg.TraceExporter}); err != nil {
		log.Fatal(err)
	}

	db := mustOpenDB(cfg.MetadataDBURL, func(c *pgxpool.Config) {
		obs.InstrumentPool(c, "metadata", slog.LevelDebug)
	})
	defer db.Close()

	view, err := web.NewRenderer()
	if err != nil {
		log.Fatal(err)
	}
	cipher, err := dbmgmt.NewCipher(cfg.CredentialEncryptionKey)
	if err != nil {
		log.Fatalf("%v\nisi CREDENTIAL_ENCRYPTION_KEY di .env, bikin dengan: openssl rand -base64 32", err)
	}

	sessions := auth.NewStore(db, auth.SessionConfig{
		IdleTimeout:      cfg.SessionIdleTimeout,
		AbsoluteTimeout:  cfg.SessionAbsoluteTimeout,
		RotationInterval: cfg.SessionRotationInterval,
		GracePeriod:      cfg.SessionGracePeriod,
		Secure:           cfg.IsProduction(),
	})
	perms := access.NewStore(db)
	connections := dbmgmt.NewStore(db, cipher)
	pools := dbmgmt.NewManager(connections)
	defer pools.Close()

	authHandler := auth.NewHandler(db, sessions)
	dbHandler := dbmgmt.NewHandler(connections, pools, perms)
	adminHandler := admin.NewHandler(admin.NewStore(db), sessions)
	approvals := approval.NewService(db, perms)
	approvalHandler := approval.NewHandler(approvals)
	jobs := queue.NewService(queue.NewStore(db), perms, approvalLookup{approvals}, cfg.WorkerMaxAttempts)
	jobHandler := queue.NewHandler(jobs)
	migHandler := migration.NewHandler(migration.NewService(db, perms, pools), view)

	registerAPI := func(mux *http.ServeMux) {
		dbHandler.RegisterAPIRoutes(mux)
		adminHandler.RegisterAPIRoutes(mux)
		approvalHandler.RegisterAPIRoutes(mux)
		jobHandler.RegisterAPIRoutes(mux)
	}
	register := func(mux *http.ServeMux) {
		migHandler.RegisterRoutes(mux)
	}
	handler := web.NewRouter(web.Options{
		Sessions:      sessions,
		Auth:          authHandler,
		RegisterAPI:   registerAPI,
		RegisterPages: register,
		SPA:           web.NewSPA(os.DirFS(cfg.SPADir)),
		Secure:        cfg.IsProduction(),
	})

	log.Printf("server jalan di http://localhost:%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	log.Fatal(http.ListenAndServe(":"+cfg.AppPort, handler))
}

type approvalLookup struct {
	svc *approval.Service
}

func (a approvalLookup) Lookup(ctx context.Context, userID, approvalID uuid.UUID) (queue.Approval, error) {
	req, err := a.svc.Get(ctx, userID, approvalID)
	if errors.Is(err, approval.ErrNotFound) {
		return queue.Approval{}, queue.ErrNotFound
	}
	if err != nil {
		return queue.Approval{}, err
	}
	return queue.Approval{
		ID:          req.ID,
		MigrationID: req.MigrationID,
		DatabaseID:  req.DatabaseID,
		Action:      string(req.Action),
		Approved:    req.Status == approval.StatusApproved,
	}, nil
}
