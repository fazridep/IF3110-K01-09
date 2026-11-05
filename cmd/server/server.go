package main

import (
	"log"
	"net/http"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/admin"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/config"
	"github.com/if3110/tubes-db-platform/internal/dbmgmt"
	"github.com/if3110/tubes-db-platform/internal/migration"
	"github.com/if3110/tubes-db-platform/internal/web"
)

func runServer(cfg *config.Config) {
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

	authHandler := auth.NewHandler(db, sessions, view)
	dbHandler := dbmgmt.NewHandler(connections, pools, view, perms)
	adminHandler := admin.NewHandler(admin.NewStore(db), sessions, view)
	migHandler := migration.NewHandler(migration.NewService(db, perms, pools), view)

	register := func(mux *http.ServeMux) {
		dbHandler.RegisterRoutes(mux)
		adminHandler.RegisterRoutes(mux)
		migHandler.RegisterRoutes(mux)
	}
	handler := web.NewRouter(sessions, authHandler, register, view, cfg.IsProduction())

	log.Printf("server jalan di http://localhost:%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	log.Fatal(http.ListenAndServe(":"+cfg.AppPort, handler))
}
