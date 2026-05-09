package cmd

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go_agent/internal/controller/chat"
	"go_agent/internal/logic/ai/common"
	"go_agent/internal/logic/app"
	"go_agent/internal/logic/session/mem"
	"go_agent/utility/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// Main is the application command entry used by main.go.
var Main = command{}

type command struct{}

// Run initializes dependencies, binds GoFrame routes, and starts the HTTP server.
func (command) Run(ctx context.Context) {
	if envPath, err := loadDotEnv(); err != nil {
		log.Printf("No .env file loaded, using system default env: %v", err)
	} else {
		log.Printf("Loaded .env file: %s", envPath)
	}

	fileDir, err := g.Cfg().Get(ctx, "file_dir")
	if err != nil {
		panic(err)
	}
	common.FileDir = fileDir.String()

	redisAddr, _ := g.Cfg().Get(ctx, "redis.addr")
	redisDB, _ := g.Cfg().Get(ctx, "redis.db")
	dialTimeout, _ := g.Cfg().Get(ctx, "redis.dialTimeout")
	redisAddress := readStringSetting(os.Getenv("REDIS_ADDR"), redisAddr.String())
	redisDatabase := readIntSetting(os.Getenv("REDIS_DB"), redisDB.Int())
	redisDialTimeout := readIntSetting(os.Getenv("REDIS_DIAL_TIMEOUT"), dialTimeout.Int())

	rdb := redis.NewClient(&redis.Options{
		Addr:        redisAddress,
		DB:          redisDatabase,
		DialTimeout: time.Duration(redisDialTimeout) * time.Second,
	})

	if err := mem.InitRedis(rdb, &mem.Config{
		MaxInputTokens:         96000,
		ReserveOutputTokens:    8192,
		ReserveToolsDefault:    20000,
		SafetyTokens:           2048,
		TTL:                    30 * time.Minute,
		KeepReasoningInContext: false,
	}); err != nil {
		panic(err)
	}
	defer func() { _ = rdb.Close() }()

	application, err := app.NewApplication(&app.Config{
		RedisAddr:     redisAddress,
		RedisPassword: "",
		RedisDB:       redisDatabase,
		LogLevel:      "info",
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer application.Close()

	log.Println("Dialogue and knowledge application initialized successfully")

	s := g.Server()
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			chatController := chat.NewV1(
				application.OrchGraph,
				application.Logger,
				application.RedisClient,
				application.KnowledgeAgent,
			)
			v1Group.Bind(chatController)
		})
	})
	serverPort := readServerPort(ctx)
	serverHost := readServerHost(ctx)
	s.SetAddr(net.JoinHostPort(serverHost, strconv.Itoa(serverPort)))
	s.Run()
}

func loadDotEnv() (string, error) {
	for _, path := range dotEnvCandidates() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := godotenv.Load(path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", os.ErrNotExist
}

func dotEnvCandidates() []string {
	var candidates []string
	if explicit := strings.TrimSpace(os.Getenv("ENV_FILE")); explicit != "" {
		candidates = append(candidates, explicit)
	}

	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, ".env"),
			filepath.Join(cwd, "Back_part", ".env"),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := candidates[:0]
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func readServerPort(ctx context.Context) int {
	if port := parsePort(os.Getenv("BACKEND_PORT")); port > 0 {
		return port
	}
	if value, err := g.Cfg().Get(ctx, "server.address"); err == nil && value != nil {
		if port := parsePort(value.String()); port > 0 {
			return port
		}
	}
	return 6872
}

func readStringSetting(primaryValue, fallbackValue string) string {
	if value := strings.TrimSpace(primaryValue); value != "" {
		return value
	}
	return strings.TrimSpace(fallbackValue)
}

func readIntSetting(primaryValue string, fallbackValue int) int {
	if value := strings.TrimSpace(primaryValue); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallbackValue
}

func readServerHost(ctx context.Context) string {
	if host := strings.TrimSpace(os.Getenv("BACKEND_HOST")); host != "" {
		return host
	}
	if value, err := g.Cfg().Get(ctx, "server.address"); err == nil && value != nil {
		if host := parseHost(value.String()); host != "" {
			return host
		}
	}
	return "0.0.0.0"
}

func parseHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return strings.TrimSpace(host)
	}
	if strings.HasPrefix(value, ":") {
		return ""
	}
	if !strings.Contains(value, ":") {
		return ""
	}
	return strings.TrimSpace(value[:strings.LastIndex(value, ":")])
}

func parsePort(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if strings.Contains(value, ":") {
		if _, port, err := net.SplitHostPort(value); err == nil {
			value = port
		} else {
			value = strings.TrimPrefix(value, ":")
		}
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}
