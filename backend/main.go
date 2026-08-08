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
	if cfg.Configured {
		if err := database.InitAdmin(cfg.Admin.Username, cfg.Admin.Password); err != nil {
			log.Fatalf("管理员初始化失败: %v", err)
		}
	}

	authService := services.NewAuthService(cfg.JWT.Secret, cfg.JWT.Expire)
	bangumiService := services.NewBangumiService()
	scannerService := services.NewScannerService(cfg.Video.RootPath)
	authHandler := handlers.NewAuthHandler(authService)
	adminUserHandler := handlers.NewAdminUserHandler()
	animeHandler := handlers.NewAnimeHandler(bangumiService, scannerService)
	bangumiHandler := handlers.NewBangumiHandler(bangumiService)
	episodeHandler := handlers.NewEpisodeHandler(authService, scannerService)
	libraryService := services.NewLibraryService(bangumiService, cfg.Video.RootPath)
	libraryHandler := handlers.NewLibraryHandler(libraryService)
	setupHandler := handlers.NewSetupHandler("config.yaml", cfg, authService, scannerService, libraryService)
	updateHandler := handlers.NewUpdateHandler(AppVersion)
	loginRateLimiter := middleware.NewLoginRateLimiter(5, time.Minute)

	r := gin.Default()
	r.Use(middleware.CORS())
	if err := r.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		log.Fatalf("配置可信代理失败: %v", err)
	}

	api := r.Group("/api")
	api.Use(middleware.RequireSetup(func() bool { return cfg.Configured }))
	{
		api.GET("/health", handlers.Health)
		api.GET("/version", updateHandler.Version)
		api.GET("/setup/status", setupHandler.Status)
		api.POST("/setup", setupHandler.Submit)
		api.GET("/episodes/:id/stream", episodeHandler.Stream)
		api.GET("/episodes/:id/subtitles", episodeHandler.Subtitles)

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
		protected.GET("/animes/:id/cover", animeHandler.Cover)
		protected.POST("/animes", animeHandler.Create)
		protected.PUT("/animes/:id", animeHandler.Update)
		protected.DELETE("/animes/:id", animeHandler.Delete)
		protected.POST("/animes/:id/scan", animeHandler.Scan)
		protected.GET("/animes/:id/episodes", animeHandler.Episodes)
		protected.POST("/library/scan", libraryHandler.Scan)

		protected.GET("/bangumi/search", bangumiHandler.Search)
		protected.GET("/bangumi/subject/:id", bangumiHandler.Subject)

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
	if err := http.Serve(listener, r); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
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
