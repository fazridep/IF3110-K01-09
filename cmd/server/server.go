package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/admin"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/config"
	"github.com/if3110/tubes-db-platform/internal/dbmgmt"
	"github.com/if3110/tubes-db-platform/internal/migration"
	"github.com/if3110/tubes-db-platform/internal/obs"
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

	db := mustOpenDB(cfg.MetadataDBURL)
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
	dbHandler := dbmgmt.NewHandler(connections, pools, view, perms)
	adminHandler := admin.NewHandler(admin.NewStore(db), sessions, view)
	migHandler := migration.NewHandler(migration.NewService(db, perms, pools), view)

	register := func(mux *http.ServeMux) {
		dbHandler.RegisterRoutes(mux)
		adminHandler.RegisterRoutes(mux)
		migHandler.RegisterRoutes(mux)
	}
	handler := web.NewRouter(web.Options{
		Sessions:      sessions,
		Auth:          authHandler,
		RegisterAPI:   func(mux *http.ServeMux) {},
		RegisterPages: register,
		SPA:           web.NewSPA(os.DirFS(cfg.SPADir)),
		Secure:        cfg.IsProduction(),
	})

	log.Printf("server jalan di http://localhost:%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	log.Fatal(http.ListenAndServe(":"+cfg.AppPort, handler))
}
