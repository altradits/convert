package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"yebente/internal/config"
	"yebente/internal/handler"
	"yebente/internal/repository/postgres"
	"yebente/internal/service"
)

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Api-Key")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("Starting Yebente Multi-Provider Settlement Gateway API...")

	// 1. Load Application Configuration
	cfg, err := config.LoadConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 2. Initialize Database Connection
	db, err := postgres.NewPostgresDB(cfg.DatabaseURL)
	if err != nil {
		log.Printf("Notice: Running in memory/testbed mode (Database offline: %v)", err)
	} else {
		defer db.Close()
		log.Println("Database connection established successfully.")
	}

	// 3. Initialize Multi-Lightning Engine
	var lndInterface service.LightningServiceInterface
	if len(cfg.LNDTLSCert) > 0 && cfg.LNDMacaroonHex != "" && cfg.LNDGRPCAddress != "" {
		client, err := service.NewLNDService(cfg.LNDGRPCAddress, cfg.LNDTLSCert, cfg.LNDMacaroonHex)
		if err != nil {
			log.Printf("Warning: Failed to connect to LND node at %s: %v", cfg.LNDGRPCAddress, err)
		} else {
			defer client.Close()
			lndInterface = client
			log.Printf("LND gRPC client connected to %s", cfg.LNDGRPCAddress)
		}
	}

	lightningEngine := service.NewMultiLightningEngine(lndInterface)

	// Check environment variables for pre-configured providers
	if lnbitsURL := os.Getenv("LNBITS_URL"); lnbitsURL != "" {
		if lnbitsKey := os.Getenv("LNBITS_ADMIN_KEY"); lnbitsKey != "" {
			lightningEngine.SetProvider(service.NewLNbitsProvider(lnbitsURL, lnbitsKey))
			log.Printf("Connected to LNbits wallet at %s", lnbitsURL)
		}
	} else if blinkKey := os.Getenv("BLINK_API_KEY"); blinkKey != "" {
		lightningEngine.SetProvider(service.NewBlinkProvider(blinkKey))
		log.Printf("Connected to Blink Lightning API")
	} else if lndRESTURL := os.Getenv("LND_REST_URL"); lndRESTURL != "" {
		if lndMac := os.Getenv("LND_MACAROON_HEX"); lndMac != "" {
			lightningEngine.SetProvider(service.NewLNDRESTProvider(lndRESTURL, lndMac))
			log.Printf("Connected to LND REST Node at %s", lndRESTURL)
		}
	}

	// 4. Initialize Daraja Service
	darajaService := service.NewDarajaService(
		cfg.DarajaBaseURL,
		cfg.DarajaConsumerKey,
		cfg.DarajaConsumerSecret,
		cfg.DarajaPasskey,
		cfg.DarajaShortCode,
		cfg.DarajaCallbackURL,
	)

	// 5. Initialize Repositories & Services
	userRepo := postgres.NewUserRepository(db)
	paymentRepo := postgres.NewPaymentRepository(db)

	settlementService := service.NewSettlementService(
		darajaService,
		lightningEngine,
		paymentRepo,
		userRepo,
		cfg.DefaultKESPerBTC,
	)

	// 6. Initialize Handlers
	authHandler := handler.NewAuthHandler(userRepo)
	mpesaHandler := handler.NewMpesaHandler(paymentRepo, lightningEngine)
	paymentHandler := handler.NewPaymentHandler(settlementService, lightningEngine, paymentRepo)

	// 7. Setup Router & Routes
	router := gin.Default()
	router.Use(corsMiddleware())

	// Serve static web app if available
	if _, err := os.Stat("./web/index.html"); err == nil {
		router.StaticFile("/", "./web/index.html")
		router.StaticFile("/index.html", "./web/index.html")
	} else if _, err := os.Stat("../index.html"); err == nil {
		router.StaticFile("/", "../index.html")
		router.StaticFile("/index.html", "../index.html")
	} else if _, err := os.Stat("index.html"); err == nil {
		router.StaticFile("/", "index.html")
		router.StaticFile("/index.html", "index.html")
	}

	// Health check
	router.GET("/healthz", func(c *gin.Context) {
		activeProv := "None"
		if prov := lightningEngine.GetActiveProvider(); prov != nil {
			activeProv = prov.Name()
		}
		c.JSON(http.StatusOK, gin.H{
			"status":             "UP",
			"environment":        cfg.Environment,
			"lightning_provider": activeProv,
			"timestamp":          time.Now().UTC(),
		})
	})

	// Public Webhook for Daraja Callback
	router.POST("/v1/mpesa/callback", mpesaHandler.HandleSTKCallback)

	// API v1 routes
	apiV1 := router.Group("/api/v1")
	{
		// Callback alias in API group
		apiV1.POST("/mpesa/callback", mpesaHandler.HandleSTKCallback)

		// Auth
		auth := apiV1.Group("/auth")
		{
			auth.POST("/request-otp", authHandler.RequestOTP)
			auth.POST("/verify-otp", authHandler.VerifyOTP)
		}

		// Payments & Deposits
		payments := apiV1.Group("/payments")
		{
			payments.POST("/deposit", paymentHandler.InitiateDeposit)
			payments.GET("/:id/status", paymentHandler.GetStatus)
			payments.POST("/instant-settle", paymentHandler.InstantSettle)
		}

		// Lightning Tools & Provider Config
		lightning := apiV1.Group("/lightning")
		{
			lightning.POST("/decode-invoice", paymentHandler.DecodeInvoice)
			lightning.POST("/resolve-address", paymentHandler.ResolveLightningAddress)
			lightning.POST("/config", paymentHandler.ConfigureLightning)
			lightning.GET("/config", paymentHandler.GetLightningConfig)
		}

		// Rates
		apiV1.GET("/rates", paymentHandler.GetRates)
	}

	// 8. Start HTTP Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Server listening on port %s in %s mode", cfg.Port, cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// 9. Graceful Shutdown
	<-ctx.Done()
	log.Println("Shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited cleanly.")
}
