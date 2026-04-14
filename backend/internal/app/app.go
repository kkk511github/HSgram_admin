package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"hsgram-admin/backend/internal/auth"
	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/config"
	"hsgram-admin/backend/internal/httpapi"
	"hsgram-admin/backend/internal/messenger"
	"hsgram-admin/backend/internal/store"
)

type App struct {
	cfg         config.Config
	store       *store.Store
	tokenMgr    *auth.Manager
	msgClient   *messenger.Client
	broadcaster *broadcast.Service
	handler     http.Handler
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	userStore, err := store.Open(cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	if err := userStore.EnsureSchema(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureReleaseSchema(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureBootstrapAdmin(ctx, cfg.BootstrapUsername, cfg.BootstrapPassword); err != nil {
		_ = userStore.Close()
		return nil, err
	}

	tokenMgr := auth.NewManager(cfg.JWTSecret, cfg.TokenTTL)
	var msgClient *messenger.Client
	var broadcaster *broadcast.Service
	if cfg.EnableBroadcasts {
		if err := userStore.EnsureBroadcastSchema(ctx); err != nil {
			_ = userStore.Close()
			return nil, err
		}
		if err := userStore.EnsureBroadcastSystemUser(ctx); err != nil {
			_ = userStore.Close()
			return nil, err
		}

		if cfg.MsgRPCAddr != "" {
			msgClient, err = messenger.New(cfg.MsgRPCAddr)
			if err != nil {
				_ = userStore.Close()
				return nil, err
			}
			broadcaster = broadcast.New(userStore, msgClient)
		} else {
			log.Printf("admin-api: ADMIN_ENABLE_BROADCASTS is true but ADMIN_MSG_RPC_ADDR is empty; broadcast API stays disabled until msg RPC is set")
		}
	}

	handler := httpapi.New(tokenMgr, userStore, broadcaster, httpapi.UpdateConfig{
		ReleasesDir:   cfg.ReleasesDir,
		PublicBaseURL: cfg.PublicBaseURL,
	})

	return &App{
		cfg:         cfg,
		store:       userStore,
		tokenMgr:    tokenMgr,
		msgClient:   msgClient,
		broadcaster: broadcaster,
		handler:     handler,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:              a.cfg.ListenAddr,
		Handler:           a.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       a.cfg.HTTPReadTimeout,
		WriteTimeout:      a.cfg.HTTPWriteTimeout,
	}

	errCh := make(chan error, 1)
	if a.broadcaster != nil {
		go a.broadcaster.Run(ctx)
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	if err := <-errCh; err != nil {
		return fmt.Errorf("serve admin api: %w", err)
	}

	return nil
}

func (a *App) Close() error {
	if err := a.msgClient.Close(); err != nil {
		_ = a.store.Close()
		return err
	}
	return a.store.Close()
}
