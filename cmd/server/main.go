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

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/config"
	"line-fleet-dispatch/internal/database"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/handler"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/notify"
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
	// F9-4：確認 statement_timeout 已生效（pooled 連線的 runtime 設定），順便 ops 可見。
	if cfg.DBStatementTimeoutMs > 0 {
		var st string
		if err := db.Raw("SHOW statement_timeout").Scan(&st).Error; err == nil {
			log.Info().Str("statement_timeout", st).Msg("DB 每條 SQL 逾時保護已啟用")
		}
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
	deviceTokenRepo := repository.NewDeviceTokenRepository(db)
	rideEventRepo := repository.NewRideEventRepository(db)
	feeSettingsRepo := repository.NewFeeSettingsRepository(db)
	membershipInvoiceRepo := repository.NewMembershipInvoiceRepository(db)
	rideMessageRepo := repository.NewRideMessageRepository(db)
	rideStopRepo := repository.NewRideStopRepository(db)
	lostItemRepo := repository.NewLostItemRepository(db)

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

	dispatchSettings := service.NewDispatchSettings(
		cfg.DispatchRadiusM, cfg.DispatchMaxDrivers,
		cfg.DispatchOfferTimeoutSec, cfg.DispatchMaxAttempts, 5,
	)

	// 費率設定：自 DB 載入單列（migration 已種下），供完成計費與後台設定使用。
	feeSettings, err := service.NewFeeSettings(feeSettingsRepo)
	if err != nil {
		log.Fatal().Err(err).Msg("載入費率設定失敗")
	}

	// Services
	dispatchService := service.NewDispatchService(
		driverRepo, rideRepo, customerRepo, redisStore, lineClient, etaService,
		dispatchSettings,
		hub,
	)
	// A2：有 FCM 憑證就用真推播，否則降級成 LogPusher。
	// **憑證問題不可讓服務起不來**——初始化失敗只打 log、照樣用 stub，派單路徑不受影響。
	var appPusher notify.AppPusher = notify.LogPusher{}
	if cfg.FCMCredentialsFile != "" {
		if fcm, err := notify.NewFCMPusher(context.Background(), cfg.FCMCredentialsFile); err != nil {
			log.Error().Err(err).Msg("FCM 初始化失敗，App 推播降級為 stub（派單不受影響）")
		} else {
			appPusher = fcm
			log.Info().Msg("FCM 推播已啟用")
		}
	} else {
		log.Info().Msg("未設定 FCM_CREDENTIALS_FILE，App 推播使用 stub")
	}
	appNotify := notify.NewDispatcher(deviceTokenRepo, appPusher)
	dispatchService.SetAppNotifier(appNotify)
	dispatchService.SetRideEvents(rideEventRepo)
	// N4：ride.assigned／ride.accepted 帶全程停靠點。漏了這行 WS payload 就不帶 stops，
	// 司機 App 接單當下看不到全程清單與多點地圖（App 模擬器實跑抓到）。
	dispatchService.SetStops(rideStopRepo)
	deviceTokenService := service.NewDeviceTokenService(deviceTokenRepo)
	rideService := service.NewRideService(customerRepo, rideRepo, redisStore, dispatchService)
	rideService.SetStops(rideStopRepo) // N3：多乘客／多停靠點行程
	rideService.SetRideEvents(rideEventRepo)
	trackingService := service.NewTrackingService(
		driverRepo, rideRepo, trackRepo, redisStore, lineClient, dispatchService,
		cfg.ETAPushMinIntervalSec, cfg.ETAPushDistThresholdM,
		hub,
	)
	trackingService.SetRideEvents(rideEventRepo)
	trackingService.SetFeeSettings(feeSettings)
	trackingService.SetOSRM(osrm)          // F3：軌跡里程偏低時以 OSRM 路線里程作計費地板
	trackingService.SetReports(reportRepo) // F9-3：完成時重算每日彙總 daily_driver_earnings
	trackingService.SetStops(rideStopRepo) // N5：多停靠點行程改用全程多點路線計費
	driverRegistry := service.NewDriverRegistry(driverRepo)
	rideQueryService := service.NewRideQueryService(trackRepo, rideRepo)
	rideQueryService.SetDrivers(driverRepo) // O4／O7：乘客查自己訂單時附司機姓名／電話
	rideQueryService.SetStops(rideStopRepo) // N6：司機端 active 帶全程停靠點
	rideStopService := service.NewRideStopService(rideRepo, rideStopRepo)
	chatService := service.NewChatService(rideRepo, rideMessageRepo, hub)
	lostItemService := service.NewLostItemService(rideRepo, lostItemRepo, feeSettings, hub)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	// Handlers
	healthHandler := handler.NewHealthHandler(db, redisClient)
	lineHandler := handler.NewLineWebhookHandler(rideService, dispatchService, driverRepo, lineClient)
	driverHandler := handler.NewDriverHandler(trackingService, driverRegistry, rideQueryService, cfg.JWTSecret, cfg.JWTExpiryHours)
	driverHandler.SetEarnings(reportRepo, feeSettings)
	rideHandler := handler.NewRideHandler(dispatchService, trackingService, rideQueryService, rideService)
	rideHandler.SetStops(rideStopService) // N7：司機標記到達／跳過停靠點
	deviceTokenHandler := handler.NewDeviceTokenHandler(deviceTokenService)
	wsHandler := handler.NewWSHandler(hub, cfg.JWTSecret, cfg.WSWriteWaitSec, cfg.WSPongWaitSec, cfg.WSMaxMessageBytes)
	chatHandler := handler.NewChatHandler(chatService)
	lostItemHandler := handler.NewLostItemHandler(lostItemService)

	// 後台：管理員 repo/service/handler，並依環境變數種一個管理員（僅在尚無 admin 時）
	adminRepo := repository.NewAdminRepository(db)
	adminRegistry := service.NewAdminRegistry(adminRepo)
	if err := adminRegistry.EnsureSeed(context.Background(), cfg.AdminSeedUsername, cfg.AdminSeedPassword); err != nil {
		log.Error().Err(err).Msg("建立種子管理員失敗")
	}
	adminUsers := service.NewAdminUsers(adminRepo)
	adminHandler := handler.NewAdminHandler(
		adminRegistry,
		service.NewAdminOperations(driverRepo, dispatchService, redisStore, dispatchSettings),
		dispatchSettings,
		driverRepo, rideRepo, trackRepo, rideEventRepo, reportRepo, adminRepo, adminUsers, redisStore,
		cfg.JWTSecret, cfg.JWTExpiryHours,
	)
	adminHandler.SetFeeSettings(feeSettings)
	adminHandler.SetMembershipInvoices(membershipInvoiceRepo)
	adminHandler.SetLostItems(lostItemRepo)

	// 乘客認證：註冊/登入（line_user_id + 密碼 JWT）
	customerRegistry := service.NewCustomerRegistry(customerRepo)
	customerHandler := handler.NewCustomerHandler(customerRegistry, cfg.JWTSecret, cfg.JWTExpiryHours)
	customerHandler.SetFeeSettings(feeSettings) // P5：乘客可讀的唯讀費率（白名單輸出）

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
			customerAuthed.GET("/customer/fees", customerHandler.Fees)
			customerAuthed.GET("/customer/rides", rideHandler.HistoryByCustomer)
			customerAuthed.GET("/customer/rides/active", rideHandler.ActiveByCustomer)
			customerAuthed.GET("/customer/rides/:id", rideHandler.GetByCustomer)
			customerAuthed.POST("/rides/:id/cancel-by-customer", rideHandler.CancelByCustomer)
			customerAuthed.POST("/customer/device-token", deviceTokenHandler.RegisterByCustomer)
			customerAuthed.DELETE("/customer/device-token", deviceTokenHandler.UnregisterByCustomer)
			// 遺失物協尋：對已完成行程建單、查自己的協尋、支付處理費
			customerAuthed.POST("/rides/:id/lost-items", lostItemHandler.CreateByCustomer)
			customerAuthed.GET("/customer/lost-items", lostItemHandler.ListByCustomer)
			customerAuthed.POST("/lost-items/:id/pay", lostItemHandler.Pay)
		}

		// 受 JWT 保護：司機操作（driver_id 取自 token，不信任 body）
		authed := api.Group("")
		authed.Use(middleware.DriverAuth(cfg.JWTSecret))
		{
			authed.GET("/driver/me", driverHandler.Me)
			authed.GET("/driver/vehicle", driverHandler.Vehicle)
			authed.PUT("/driver/vehicle", driverHandler.UpdateVehicle)
			authed.GET("/driver/earnings", driverHandler.Earnings)
			authed.POST("/driver/online", driverHandler.Online)
			authed.POST("/driver/offline", driverHandler.Offline)
			authed.GET("/driver/rides/active", driverHandler.ActiveRide)
			authed.POST("/driver/location", driverHandler.ReportLocation)
			authed.POST("/driver/device-token", deviceTokenHandler.RegisterByDriver)
			authed.DELETE("/driver/device-token", deviceTokenHandler.UnregisterByDriver)
			authed.POST("/rides/:id/accept", rideHandler.Accept)
			authed.POST("/rides/:id/pickup", rideHandler.PickUp)
			// N7：多停靠點行程的到達／跳過標記（被指派司機限定）
			authed.POST("/rides/:id/stops/:stop_id/arrive", rideHandler.ArriveStop)
			authed.POST("/rides/:id/stops/:stop_id/skip", rideHandler.SkipStop)
			authed.POST("/rides/:id/complete", rideHandler.Complete)
			authed.POST("/rides/:id/cancel", rideHandler.Cancel)
			authed.POST("/rides/:id/decline", rideHandler.Decline)
			// 遺失物協尋：司機工作清單、標記尋獲/歸還
			authed.GET("/driver/lost-items", lostItemHandler.ListByDriver)
			authed.POST("/lost-items/:id/found", lostItemHandler.MarkFound)
			authed.POST("/lost-items/:id/return", lostItemHandler.MarkReturned)
		}

		// 軌跡回放：受多角色 JWT 保護，僅本趟乘客／司機／admin 可存取（授權在 handler）
		api.GET("/rides/:id/track", middleware.MultiAuth(cfg.JWTSecret), rideHandler.Track)

		// 行程內對話：歷史查詢（乘客/司機/admin）＋發話（僅乘客/司機）；即時遞送走 /ws 的 chat.message
		api.GET("/rides/:id/messages", middleware.MultiAuth(cfg.JWTSecret), chatHandler.List)
		api.POST("/rides/:id/messages", middleware.MultiAuth(cfg.JWTSecret), chatHandler.Send)
		// 遺失物協尋：查單趟協尋單（乘客/司機/admin）＋結案（乘客/司機）
		api.GET("/rides/:id/lost-items", middleware.MultiAuth(cfg.JWTSecret), lostItemHandler.GetByRide)
		api.POST("/lost-items/:id/close", middleware.MultiAuth(cfg.JWTSecret), lostItemHandler.Close)

		// 後台：登入公開，其餘受 admin JWT 保護
		api.POST("/admin/login", adminHandler.Login)
		adminG := api.Group("/admin")
		adminG.Use(middleware.AdminAuth(cfg.JWTSecret, func(id int64) (string, bool, error) {
			a, err := adminRepo.FindByID(id)
			if err != nil {
				return "", false, err
			}
			return a.Role, a.IsActive, nil
		}))
		{
			adminG.GET("/me", adminHandler.Me)
			// viewer：唯讀
			read := adminG.Group("")
			read.Use(middleware.RequireAdminRole(auth.RoleViewer))
			{
				read.GET("/fleet", adminHandler.Fleet)
				read.GET("/drivers", adminHandler.Drivers)
				read.GET("/rides", adminHandler.Rides)
				read.GET("/rides/:id", adminHandler.RideDetail)
				read.GET("/reports/daily", adminHandler.DailyReport)
				read.GET("/reports/monthly", adminHandler.MonthlyReport)
				read.GET("/membership-invoices", adminHandler.ListMembershipInvoices)
				read.GET("/lost-items", adminHandler.ListLostItems)
				read.GET("/settings/dispatch", adminHandler.GetDispatchSettings)
			}
			// dispatcher：派單操作
			ops := adminG.Group("")
			ops.Use(middleware.RequireAdminRole(auth.RoleDispatcher))
			{
				ops.PATCH("/drivers/:id/status", adminHandler.PatchDriverStatus)
				ops.PUT("/settings/dispatch", adminHandler.PutDispatchSettings)
				ops.POST("/rides/:id/cancel", adminHandler.CancelRide)
			}
			// superadmin：帳號管理
			sup := adminG.Group("/admins")
			sup.Use(middleware.RequireAdminRole(auth.RoleSuperadmin))
			{
				sup.GET("", adminHandler.ListAdmins)
				sup.POST("", adminHandler.CreateAdmin)
				sup.PATCH("/:id", adminHandler.UpdateAdmin)
			}
			// superadmin：費率設定（手續費/會費/車資費率）
			supSettings := adminG.Group("")
			supSettings.Use(middleware.RequireAdminRole(auth.RoleSuperadmin))
			{
				supSettings.GET("/settings/fees", adminHandler.GetFeeSettings)
				supSettings.PUT("/settings/fees", adminHandler.PutFeeSettings)
				supSettings.POST("/membership-invoices/generate", adminHandler.GenerateMembershipInvoices)
				supSettings.PATCH("/membership-invoices/:id", adminHandler.MarkMembershipInvoicePaid)
			}
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
