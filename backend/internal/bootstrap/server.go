package bootstrap

import (
	"context"
	"log"

	appconfig "go_agent/internal/config"
	"go_agent/internal/controller/chat"
	"go_agent/utility/common"
	"go_agent/utility/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/joho/godotenv"
)

func RunHTTPServer() error {
	ctx := gctx.New()
	loadDotEnv()

	cfg, err := LoadConfigFromGoFrame(ctx)
	if err != nil {
		return err
	}
	app, err := NewApplication(cfg)
	if err != nil {
		return err
	}
	defer app.Close()

	log.Println("Agent architecture initialized successfully")
	log.Printf("Incident workflow agent ready")
	log.Printf("Prometheus URL: %s", cfg.Typed.Runtime.PrometheusURL)

	server := g.Server()
	server.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			runtime := app.Runtime
			chatController := chat.NewV1FromDeps(chat.ControllerDeps{
				DialogueAgent:    app.DialogueAgent,
				ChatStreamRunner: runtime.ChatRunner,
				OpsStreamRunner:  runtime.OpsRunner,
				RootAgentName:    runtime.RootAgentName,
				OpsRootAgentName: runtime.OpsRootName,
				SessionMemory:    runtime.SessionMemory,
				SlashRegistry:    runtime.SlashRegistry,
				WorkDir:          runtime.WorkDir,
				Logger:           app.Logger,
				OpsAgent:         app.OpsAgent,
				KnowledgeAgent:   app.KnowledgeAgent,
				HookEngine:       app.HookEngine,
				Telemetry:        app.Telemetry,
			})
			v1Group.Bind(chatController)
		})
	})

	server.SetPort(6872)
	server.Run()
	return nil
}

func LoadConfigFromGoFrame(ctx context.Context) (*Config, error) {
	loaded, err := appconfig.LoadGoFrame(ctx)
	if err != nil {
		return nil, err
	}
	common.FileDir = loaded.FileDir

	return &Config{Typed: loaded.Config}, nil
}

func loadDotEnv() {
	for _, envFile := range []string{".env", "../.env"} {
		if err := godotenv.Load(envFile); err == nil {
			return
		}
	}
	log.Println("Error loading .env file, using system default env")
}
