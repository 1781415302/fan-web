package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/config"
	"fan-web/database"
	"fan-web/handlers"
	"fan-web/middleware"
	"fan-web/services"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	gin.SetMode(cfg.Server.Mode)

	if err := database.Init(cfg.Database.Path); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	if err := database.InitAdmin(cfg.Admin.Username, cfg.Admin.Password); err != nil {
		log.Fatalf("管理员初始化失败: %v", err)
	}

	authService := services.NewAuthService(cfg.JWT.Secret, cfg.JWT.Expire)
	bangumiService := services.NewBangumiService()
	scannerService := services.NewScannerService(cfg.Video.RootPath)
	authHandler := handlers.NewAuthHandler(authService)
	adminUserHandler := handlers.NewAdminUserHandler()
	animeHandler := handlers.NewAnimeHandler(bangumiService, scannerService)
	bangumiHandler := handlers.NewBangumiHandler(bangumiService)
	episodeHandler := handlers.NewEpisodeHandler(authService, scannerService)
	loginRateLimiter := middleware.NewLoginRateLimiter(5, time.Minute)

	r := gin.Default()
	r.Use(middleware.CORS())
	if err := r.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		log.Fatalf("配置可信代理失败: %v", err)
	}

	api := r.Group("/api")
	{
		api.GET("/health", handlers.Health)
		api.GET("/episodes/:id/stream", episodeHandler.Stream)

		auth := api.Group("/auth")
		auth.POST("/login", loginRateLimiter.Middleware(), authHandler.Login)

		protected := api.Group("")
		protected.Use(middleware.JWTAuth(authService))
		protected.GET("/auth/me", authHandler.Me)
		protected.POST("/auth/logout", authHandler.Logout)
		protected.GET("/progress/anime/:anime_id", episodeHandler.AnimeProgress)
		protected.GET("/progress/:episode_id", episodeHandler.GetProgress)
		protected.POST("/progress/:episode_id", episodeHandler.ReportProgress)

		protected.GET("/animes", animeHandler.List)
		protected.GET("/animes/:id", animeHandler.Get)
		protected.POST("/animes", animeHandler.Create)
		protected.PUT("/animes/:id", animeHandler.Update)
		protected.DELETE("/animes/:id", animeHandler.Delete)
		protected.POST("/animes/:id/scan", animeHandler.Scan)
		protected.GET("/animes/:id/episodes", animeHandler.Episodes)

		protected.GET("/bangumi/search", bangumiHandler.Search)
		protected.GET("/bangumi/subject/:id", bangumiHandler.Subject)

		admin := protected.Group("/admin")
		admin.Use(middleware.RequireAdmin)
		admin.GET("/users", adminUserHandler.List)
		admin.POST("/users", adminUserHandler.Create)
		admin.DELETE("/users/:id", adminUserHandler.Delete)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("服务启动，监听 %s\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
