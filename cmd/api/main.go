package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	httpapi "github.com/hases/hases-api/internal/adapters/http"
	"github.com/hases/hases-api/internal/adapters/persistence"
	"github.com/hases/hases-api/internal/app/mailer"
	"github.com/hases/hases-api/internal/app/notifier"
	"github.com/hases/hases-api/internal/auth"
	"github.com/hases/hases-api/internal/config"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := os.MkdirAll(cfg.StorageDir, 0o755); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := persistence.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := persistence.RunMigrations(ctx, pool); err != nil {
		log.Fatal("migrations: ", err)
	}

	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "admin@local.test"
	}
	adminPass := os.Getenv("ADMIN_INITIAL_PASSWORD")
	if adminPass == "" {
		adminPass = "admin123"
	}
	hash, err := auth.HashPassword(adminPass)
	if err != nil {
		log.Fatal(err)
	}
	if err := persistence.EnsureAdmin(ctx, pool, adminEmail, hash); err != nil {
		log.Fatal("seed admin: ", err)
	}

	mlr := mailer.New(cfg)
	notif := notifier.New(pool, mlr)
	go notif.Run(ctx, 30*time.Second, 25)

	srv := &httpapi.Server{Pool: pool, Cfg: cfg, Mailer: mlr, Notifier: notif}
	log.Printf("listening %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, srv.Routes()))
}
