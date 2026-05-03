package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"hsgram-admin/backend/internal/auth"
	"hsgram-admin/backend/internal/authsessionrpc"
	"hsgram-admin/backend/internal/broadcast"
	"hsgram-admin/backend/internal/config"
	"hsgram-admin/backend/internal/gatewayrpc"
	"hsgram-admin/backend/internal/httpapi"
	"hsgram-admin/backend/internal/invitecodes"
	"hsgram-admin/backend/internal/messenger"
	"hsgram-admin/backend/internal/risk"
	"hsgram-admin/backend/internal/statusrpc"
	"hsgram-admin/backend/internal/stickers"
	"hsgram-admin/backend/internal/store"
	"hsgram-admin/backend/internal/syncrpc"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	redisstore "github.com/zeromicro/go-zero/core/stores/redis"
)

type App struct {
	cfg               config.Config
	store             *store.Store
	tokenMgr          *auth.Manager
	msgClient         *messenger.Client
	authsessionClient *authsessionrpc.Client
	syncClient        *syncrpc.Client
	statusClient      *statusrpc.Client
	gatewayClient     *gatewayrpc.Client
	broadcaster       *broadcast.Service
	riskService       *risk.Service
	inviteCodes       *invitecodes.Service
	stickerImporter   *stickers.Service
	handler           http.Handler
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
	if err := userStore.EnsureRuntimeSettingsSchema(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureSupportSchema(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureSupportSystemUser(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureReleaseSchema(ctx); err != nil {
		_ = userStore.Close()
		return nil, err
	}
	stickerStorage, err := stickers.NewMinIOStorage(stickers.MinIOConfig{
		Endpoint:        cfg.StickerMinIO.Endpoint,
		AccessKeyID:     cfg.StickerMinIO.AccessKeyID,
		SecretAccessKey: cfg.StickerMinIO.SecretAccessKey,
		UseSSL:          cfg.StickerMinIO.UseSSL,
		Bucket:          cfg.StickerMinIO.Bucket,
	})
	if err != nil {
		_ = userStore.Close()
		return nil, err
	}
	stickerImporter, err := stickers.New(ctx, stickers.Config{
		DatabaseDSN:      cfg.DatabaseDSN,
		TelegramBotToken: cfg.TelegramBotToken,
		Storage:          stickerStorage,
	})
	if err != nil {
		_ = userStore.Close()
		return nil, err
	}
	if err := userStore.EnsureBootstrapAdmin(ctx, cfg.BootstrapUsername, cfg.BootstrapPassword); err != nil {
		_ = userStore.Close()
		return nil, err
	}

	tokenMgr := auth.NewManager(cfg.JWTSecret, cfg.TokenTTL)
	kvStore := kv.NewStore(cache.ClusterConf{{RedisConf: redisstore.RedisConf{Host: cfg.RedisAddr, Pass: cfg.RedisPass, Type: "node"}, Weight: 100}})
	riskService := risk.New(kvStore)
	inviteService := invitecodes.New(kvStore)
	var msgClient *messenger.Client
	if cfg.MsgRPCAddr != "" {
		msgClient, err = messenger.New(cfg.MsgRPCAddr)
		if err != nil {
			_ = userStore.Close()
			return nil, err
		}
	} else {
		log.Printf("admin-api: ADMIN_MSG_RPC_ADDR is empty; support reply and broadcast APIs stay disabled until msg RPC is set")
	}
	if persistedInviteSettings, found, err := userStore.GetInviteCodeSettings(ctx); err != nil {
		if msgClient != nil {
			_ = msgClient.Close()
		}
		_ = userStore.Close()
		return nil, err
	} else if found {
		if _, err = inviteService.UpdateSettings(ctx, persistedInviteSettings.Enabled); err != nil {
			if msgClient != nil {
				_ = msgClient.Close()
			}
			_ = userStore.Close()
			return nil, err
		}
	} else if currentInviteSettings, err := inviteService.GetSettings(ctx); err == nil && currentInviteSettings.UpdatedAt != 0 {
		if _, err := userStore.SaveInviteCodeSettings(ctx, currentInviteSettings.Enabled); err != nil {
			log.Printf("admin-api: backfill invite settings failed: %v", err)
		}
	}
	var authsessionClient *authsessionrpc.Client
	var syncClient *syncrpc.Client
	var statusClient *statusrpc.Client
	var gatewayClient *gatewayrpc.Client
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

		if msgClient != nil {
			broadcaster = broadcast.New(userStore, msgClient)
		} else {
			log.Printf("admin-api: ADMIN_ENABLE_BROADCASTS is true but ADMIN_MSG_RPC_ADDR is empty; broadcast API stays disabled until msg RPC is set")
		}
	}

	if cfg.AuthsessionRPCAddr != "" {
		authsessionClient, err = authsessionrpc.New(cfg.AuthsessionRPCAddr)
		if err != nil {
			if msgClient != nil {
				_ = msgClient.Close()
			}
			_ = userStore.Close()
			return nil, err
		}
	} else {
		log.Printf("admin-api: ADMIN_AUTHSESSION_RPC_ADDR is empty; session revocation falls back to database-only mode")
	}

	if cfg.SyncRPCAddr != "" {
		syncClient, err = syncrpc.New(cfg.SyncRPCAddr)
		if err != nil {
			if authsessionClient != nil {
				_ = authsessionClient.Close()
			}
			if msgClient != nil {
				_ = msgClient.Close()
			}
			_ = userStore.Close()
			return nil, err
		}
	} else {
		log.Printf("admin-api: ADMIN_SYNC_RPC_ADDR is empty; online forced logout popup will be disabled")
	}

	if cfg.StatusRPCAddr != "" {
		statusClient, err = statusrpc.New(cfg.StatusRPCAddr)
		if err != nil {
			if syncClient != nil {
				_ = syncClient.Close()
			}
			if authsessionClient != nil {
				_ = authsessionClient.Close()
			}
			if msgClient != nil {
				_ = msgClient.Close()
			}
			_ = userStore.Close()
			return nil, err
		}
	} else {
		log.Printf("admin-api: ADMIN_STATUS_RPC_ADDR is empty; online session lookup will be disabled")
	}

	if cfg.GatewayRPCAddr != "" {
		gatewayClient, err = gatewayrpc.New(cfg.GatewayRPCAddr)
		if err != nil {
			if statusClient != nil {
				_ = statusClient.Close()
			}
			if syncClient != nil {
				_ = syncClient.Close()
			}
			if authsessionClient != nil {
				_ = authsessionClient.Close()
			}
			if msgClient != nil {
				_ = msgClient.Close()
			}
			_ = userStore.Close()
			return nil, err
		}
	} else {
		log.Printf("admin-api: ADMIN_GATEWAY_RPC_ADDR is empty; online socket disconnect will be disabled")
	}

	handler := httpapi.New(tokenMgr, userStore, broadcaster, authsessionClient, syncClient, statusClient, gatewayClient, riskService, inviteService, msgClient, stickerImporter, httpapi.UpdateConfig{
		ReleasesDir:   cfg.ReleasesDir,
		PublicBaseURL: cfg.PublicBaseURL,
	})

	return &App{
		cfg:               cfg,
		store:             userStore,
		tokenMgr:          tokenMgr,
		msgClient:         msgClient,
		authsessionClient: authsessionClient,
		syncClient:        syncClient,
		statusClient:      statusClient,
		gatewayClient:     gatewayClient,
		broadcaster:       broadcaster,
		riskService:       riskService,
		inviteCodes:       inviteService,
		stickerImporter:   stickerImporter,
		handler:           handler,
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
	if a.msgClient != nil {
		if err := a.msgClient.Close(); err != nil {
			if a.authsessionClient != nil {
				_ = a.authsessionClient.Close()
			}
			_ = a.store.Close()
			return err
		}
	}
	if a.authsessionClient != nil {
		if err := a.authsessionClient.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	if a.syncClient != nil {
		if err := a.syncClient.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	if a.statusClient != nil {
		if err := a.statusClient.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	if a.gatewayClient != nil {
		if err := a.gatewayClient.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	if a.stickerImporter != nil {
		if err := a.stickerImporter.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	return a.store.Close()
}
