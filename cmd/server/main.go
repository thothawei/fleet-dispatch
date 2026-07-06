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
	)
	rideService := service.NewRideService(customerRepo, rideRepo, redisStore, dispatchService)
	trackingService := service.NewTrackingService(driverRepo, rideRepo, trackRepo, redisStore, lineClient, dispatchService)
	driverRegistry := service.NewDriverRegistry(driverRepo)
	rideQueryService := service.NewRideQueryService(trackRepo)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// Handlers
	healthHandler := handler.NewHealthHandler(db, redisClient)
	lineHandler := handler.NewLineWebhookHandler(rideService, dispatchService, driverRepo, lineClient)
	driverHandler := handler.NewDriverHandler(trackingService, driverRegistry, cfg.JWTSecret, cfg.JWTExpiryHours)
	rideHandler := handler.NewRideHandler(dispatchService, trackingService, rideQueryService)
	reportHandler := handler.NewReportHandler(reportRepo)

	// Routes
	r.GET("/healthz", healthHandler.Healthz)
	r.POST("/webhook/line", middleware.LineSignature(cfg.LineChannelSecret), lineHandler.Handle)

	api := r.Group("/api")
	{
		// 公開：註冊 / 登入
		api.POST("/driver/register", driverHandler.Register)
		api.POST("/driver/login", driverHandler.Login)

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

		// 唯讀（暫不保護，未來加 admin 認證）
		api.GET("/rides/:id/track", rideHandler.Track)
		api.GET("/reports/daily", reportHandler.Daily)
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
