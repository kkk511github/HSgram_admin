package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"hsgram-admin/backend/internal/app"
	"hsgram-admin/backend/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			log.Printf("close app: %v", err)
		}
	}()

	log.Printf("HSgram admin listening on %s", cfg.ListenAddr)
	if err := application.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
