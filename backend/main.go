package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/config"
	"fan-web/database"
	"fan-web/handlers"
	"fan-web/middleware"
	"fan-web/services"
	"fan-web/web"
)

var AppVersion = "dev"

func main() {
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	portFlag := flag.Int("port", 0, "服务器端口，覆盖配置文件")
	flag.Parse()
	if *showVersion {
		fmt.Println(AppVersion)
		os.Exit(0)
	}
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if *portFlag > 0 {
		cfg.Server.Port = *portFlag
	}

	gin.SetMode(cfg.Server.Mode)

	if err := database.Init(cfg.Database.Path); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	if err := prepareConfiguredInstance("config.yaml", cfg); err != nil {
		log.Fatalf("配置安全迁移失败: %v", err)
	}

	authService := services.NewAuthService(cfg.JWT.Secret, cfg.JWT.Expire)
	bangumiService := services.NewBangumiService()
	scannerService := services.NewScannerService(cfg.Video.RootPath)
	loginRateLimiter := middleware.NewLoginRateLimiter(5, time.Minute)
	authHandler := handlers.NewAuthHandler(authService, loginRateLimiter)
	adminUserHandler := handlers.NewAdminUserHandler()
	animeHandler := handlers.NewAnimeHandler(bangumiService, scannerService)
	bangumiHandler := handlers.NewBangumiHandler(bangumiService)
	episodeHandler := handlers.NewEpisodeHandler(authService, scannerService)
	libraryService := services.NewLibraryService(bangumiService, cfg.Video.RootPath)
	libraryHandler := handlers.NewLibraryHandler(libraryService)
	setupHandler := handlers.NewSetupHandler("config.yaml", cfg, authService, scannerService, libraryService)
	updateHandler := handlers.NewUpdateHandler(AppVersion)

	r := gin.New()
	r.Use(middleware.RequestLogger(log.Writer()))
	r.Use(middleware.Recovery(log.Writer()))
	r.Use(middleware.CORS())
	r.Use(middleware.LimitJSONBody(64 << 10))
	if err := r.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		log.Fatalf("配置可信代理失败: %v", err)
	}

	api := r.Group("/api")
	api.Use(middleware.RequireSetup(setupHandler.IsConfigured))
	{
		api.GET("/health", handlers.Health)
		api.GET("/version", updateHandler.Version)
		api.GET("/setup/status", setupHandler.Status)
		api.POST("/setup", setupHandler.Submit)

		auth := api.Group("/auth")
		auth.POST("/login", loginRateLimiter.Middleware(), authHandler.Login)
		// Stream/Subtitles 自行校验 Bearer 与 media_token，不能放在 JWTAuth 组内。
		api.GET("/episodes/:id/stream", episodeHandler.Stream)
		api.GET("/episodes/:id/subtitles", episodeHandler.Subtitles)

		protected := api.Group("")
		protected.Use(middleware.JWTAuth(authService))
		protected.GET("/auth/me", authHandler.Me)
		protected.POST("/auth/logout", authHandler.Logout)
		protected.GET("/progress/anime/:anime_id", episodeHandler.AnimeProgress)
		protected.GET("/progress/:episode_id", episodeHandler.GetProgress)
		protected.POST("/progress/:episode_id", episodeHandler.ReportProgress)
		protected.POST("/episodes/:id/media-token", episodeHandler.IssueMediaToken)

		// 普通用户只读媒体库与自身进度。
		protected.GET("/animes", animeHandler.List)
		protected.GET("/animes/:id", animeHandler.Get)
		protected.GET("/animes/:id/cover", animeHandler.Cover)
		protected.GET("/animes/:id/episodes", animeHandler.Episodes)

		// 管理写操作：媒体库管理与 Bangumi 入库存放于 RequireAdmin 组。
		manager := protected.Group("")
		manager.Use(middleware.RequireAdmin)
		manager.POST("/animes", animeHandler.Create)
		manager.PUT("/animes/:id", animeHandler.Update)
		manager.DELETE("/animes/:id", animeHandler.Delete)
		manager.POST("/animes/:id/scan", animeHandler.Scan)
		manager.POST("/library/scan", libraryHandler.Scan)
		manager.GET("/bangumi/search", bangumiHandler.Search)
		manager.GET("/bangumi/subject/:id", bangumiHandler.Subject)

		admin := protected.Group("/admin")
		admin.Use(middleware.RequireAdmin)
		admin.GET("/users", adminUserHandler.List)
		admin.POST("/users", adminUserHandler.Create)
		admin.DELETE("/users/:id", adminUserHandler.Delete)
		admin.GET("/update/check", updateHandler.Check)
		admin.POST("/update/perform", updateHandler.Perform)
	}

	distFS, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}
	r.NoRoute(serveFrontend(http.FS(distFS)))

	listener, actualPort, err := listenWithFallback(cfg.Server.Port, 10)
	if err != nil {
		log.Fatalf("端口绑定失败: %v", err)
	}
	log.Printf("服务启动，监听 :%d → http://127.0.0.1:%d", actualPort, actualPort)
	// 可执行文件 .old 与数据库迁移前备份同属一套回滚资产。
	// 仅当实际绑定端口等于配置端口（含 -port 覆盖后的值）才清理；
	// 回退绑定视为启动未完全按配置成功，保留 .old 与 .pre-migration.bak。
	// 回退端口只用于本次监听，不写回配置。
	if shouldCleanupRollback(actualPort, cfg.Server.Port) {
		services.CleanupUpdateBackup()
		database.CleanupPreMigrationBackup(cfg.Database.Path)
	} else {
		log.Printf("端口回退到 %d（配置为 %d），保留 .old 与 .pre-migration.bak 以便回滚", actualPort, cfg.Server.Port)
	}
	server := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// 不设置 WriteTimeout：长视频播放与慢速 Range 响应不应被强制截断。
	}
	if err := server.Serve(listener); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// shouldCleanupRollback 仅在实际绑定端口等于配置端口时清理回滚资产。
func shouldCleanupRollback(actualPort, configuredPort int) bool {
	return actualPort == configuredPort
}

// listenWithFallback 从起始端口开始监听，端口被占用时自动顺延，最多尝试 maxAttempts 次。
func listenWithFallback(startPort, maxAttempts int) (net.Listener, int, error) {
	for i := 0; i < maxAttempts; i++ {
		port := startPort + i
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener, port, nil
		}
		if !isAddrInUse(err) {
			return nil, 0, fmt.Errorf("监听 %s 失败: %w", addr, err)
		}
		log.Printf("端口 %d 被占用，尝试 %d", port, port+1)
	}
	return nil, 0, fmt.Errorf("端口 %d~%d 均被占用", startPort, startPort+maxAttempts-1)
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return sysErr.Err == syscall.EADDRINUSE
		}
	}
	return false
}

// serveFrontend 托管嵌入的前端静态资源，并为单页应用做 index.html 回退。
func serveFrontend(fsys http.FileSystem) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "接口不存在"})
			return
		}

		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path != "" && path != "index.html" {
			if f, err := fsys.Open(path); err == nil {
				f.Close()
				c.FileFromFS(c.Request.URL.Path, fsys)
				return
			}
		}

		c.FileFromFS("/", fsys)
	}
}
