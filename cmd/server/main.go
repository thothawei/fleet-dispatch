package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/config"
	"line-fleet-dispatch/internal/database"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/handler"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/middleware"
	osrmclient "line-fleet-dispatch/internal/osrm"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrateOnly()
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("載入設定失敗")
	}

	if err := database.RunMigrations(cfg.MigrateDSN(), "db/migrations"); err != nil {
		log.Fatal().Err(err).Msg("資料庫 migration 失敗")
	}

	db, err := database.Connect(cfg.DSN(), cfg.AppEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("連線資料庫失敗")
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal().Err(err).Msg("連線 Redis 失敗")
	}

	// Repositories
	customerRepo := repository.NewCustomerRepository(db)
	driverRepo := repository.NewDriverRepository(db)
	rideRepo := repository.NewRideRepository(db)
	trackRepo := repository.NewTrackRepository(db)
	reportRepo := repository.NewReportRepository(db)

	// 軌跡分區維護：啟動時預建未來月分區 + 每日排程（避免跨月寫入失敗）
	if err := trackRepo.EnsureTrackPartitions(cfg.TrackPartitionMonthsAhead); err != nil {
		log.Error().Err(err).Msg("初始化軌跡分區失敗")
	} else {
		log.Info().Int("months_ahead", cfg.TrackPartitionMonthsAhead).Msg("軌跡分區已確保")
	}
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := trackRepo.EnsureTrackPartitions(cfg.TrackPartitionMonthsAhead); err != nil {
				log.Error().Err(err).Msg("軌跡分區維護失敗")
			}
			if dropped, err := trackRepo.DropOldTrackPartitions(cfg.TrackRetentionMonths); err != nil {
				log.Error().Err(err).Msg("清理舊軌跡分區失敗")
			} else if len(dropped) > 0 {
				log.Info().Strs("dropped", dropped).Msg("已清理舊軌跡分區")
			}
		}
	}()

	// WebSocket Hub（即時事件通道，單 goroutine 常駐路由）
	hub := events.NewHub()
	go hub.Run()

	// Infrastructure
	redisStore := redisstore.NewStore(redisClient, cfg.DriverOfflineSec)
	osrm := osrmclient.NewClient(cfg.OSRMURL)
	etaService := service.NewETAService(osrm)
	lineClient := lineclient.NewClient(cfg.LineChannelAccessToken)

	// Services
	dispatchService := service.NewDispatchService(
		driverRepo, rideRepo, customerRepo, redisStore, lineClient, etaService,
		cfg.DispatchRadiusM, cfg.DispatchMaxDrivers,
		cfg.DispatchOfferTimeoutSec, cfg.DispatchMaxAttempts,
		hub,
	)
	rideService := service.NewRideService(customerRepo, rideRepo, redisStore, dispatchService)
	trackingService := service.NewTrackingService(
		driverRepo, rideRepo, trackRepo, redisStore, lineClient, dispatchService,
		cfg.ETAPushMinIntervalSec, cfg.ETAPushDistThresholdM,
		hub,
	)
	driverRegistry := service.NewDriverRegistry(driverRepo)
	rideQueryService := service.NewRideQueryService(trackRepo, rideRepo)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// Handlers
	healthHandler := handler.NewHealthHandler(db, redisClient)
	lineHandler := handler.NewLineWebhookHandler(rideService, dispatchService, driverRepo, lineClient)
	driverHandler := handler.NewDriverHandler(trackingService, driverRegistry, cfg.JWTSecret, cfg.JWTExpiryHours)
	rideHandler := handler.NewRideHandler(dispatchService, trackingService, rideQueryService, rideService)
	wsHandler := handler.NewWSHandler(hub, cfg.JWTSecret, cfg.WSWriteWaitSec, cfg.WSPongWaitSec, cfg.WSMaxMessageBytes)

	// 後台：管理員 repo/service/handler，並依環境變數種一個管理員（僅在尚無 admin 時）
	adminRepo := repository.NewAdminRepository(db)
	adminRegistry := service.NewAdminRegistry(adminRepo)
	if err := adminRegistry.EnsureSeed(context.Background(), cfg.AdminSeedUsername, cfg.AdminSeedPassword); err != nil {
		log.Error().Err(err).Msg("建立種子管理員失敗")
	}
	adminHandler := handler.NewAdminHandler(
		adminRegistry, driverRepo, rideRepo, trackRepo, reportRepo, redisStore,
		cfg.JWTSecret, cfg.JWTExpiryHours,
	)

	// 乘客認證：註冊/登入（line_user_id + 密碼 JWT）
	customerRegistry := service.NewCustomerRegistry(customerRepo)
	customerHandler := handler.NewCustomerHandler(customerRegistry, cfg.JWTSecret, cfg.JWTExpiryHours)

	// Routes
	r.GET("/healthz", healthHandler.Healthz)
	r.GET("/ws", wsHandler.Connect)
	r.POST("/webhook/line", middleware.LineSignature(cfg.LineChannelSecret), lineHandler.Handle)

	api := r.Group("/api")
	{
		// 公開：註冊 / 登入
		api.POST("/driver/register", driverHandler.Register)
		api.POST("/driver/login", driverHandler.Login)

		// 乘客：註冊 / 登入（公開）
		api.POST("/customer/register", customerHandler.Register)
		api.POST("/customer/login", customerHandler.Login)

		// 受乘客 JWT 保護：App 下單
		customerAuthed := api.Group("")
		customerAuthed.Use(middleware.CustomerAuth(cfg.JWTSecret))
		{
			customerAuthed.POST("/rides", rideHandler.Create)
			customerAuthed.GET("/customer/rides/active", rideHandler.ActiveByCustomer)
			customerAuthed.GET("/customer/rides/:id", rideHandler.GetByCustomer)
			customerAuthed.POST("/rides/:id/cancel-by-customer", rideHandler.CancelByCustomer)
		}

		// 受 JWT 保護：司機操作（driver_id 取自 token，不信任 body）
		authed := api.Group("")
		authed.Use(middleware.DriverAuth(cfg.JWTSecret))
		{
			authed.POST("/driver/location", driverHandler.ReportLocation)
			authed.POST("/rides/:id/accept", rideHandler.Accept)
			authed.POST("/rides/:id/pickup", rideHandler.PickUp)
			authed.POST("/rides/:id/complete", rideHandler.Complete)
			authed.POST("/rides/:id/cancel", rideHandler.Cancel)
		}

		// 軌跡回放：受多角色 JWT 保護，僅本趟乘客／司機／admin 可存取（授權在 handler）
		api.GET("/rides/:id/track", middleware.MultiAuth(cfg.JWTSecret), rideHandler.Track)

		// 後台：登入公開，其餘受 admin JWT 保護
		api.POST("/admin/login", adminHandler.Login)
		adminG := api.Group("/admin")
		adminG.Use(middleware.AdminAuth(cfg.JWTSecret))
		{
			adminG.GET("/fleet", adminHandler.Fleet)
			adminG.GET("/drivers", adminHandler.Drivers)
			adminG.GET("/rides", adminHandler.Rides)
			adminG.GET("/rides/:id", adminHandler.RideDetail)
			adminG.GET("/reports/daily", adminHandler.DailyReport)
		}
	}

	// LIFF 靜態頁
	r.Static("/liff", "./web/liff")

	srvAddr := fmt.Sprintf(":%d", cfg.AppPort)
	log.Info().Str("addr", srvAddr).Msg("服務啟動")

	go func() {
		if err := r.Run(srvAddr); err != nil {
			log.Fatal().Err(err).Msg("HTTP 服務異常結束")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("收到關閉信號，正在停止...")
}

func runMigrateOnly() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("載入設定失敗")
	}
	if err := database.RunMigrations(cfg.MigrateDSN(), "db/migrations"); err != nil {
		log.Fatal().Err(err).Msg("migration 失敗")
	}
	log.Info().Msg("migration 完成")
}
