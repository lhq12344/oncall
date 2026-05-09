package app

import (
	"context"
	"os"
	"strings"
	"sync"

	ccb "github.com/cloudwego/eino-ext/callbacks/cozeloop"
	"github.com/cloudwego/eino/callbacks"
	"github.com/coze-dev/cozeloop-go"
	"go.uber.org/zap"
)

var (
	cozeLoopMu         sync.Mutex
	cozeLoopClient     cozeloop.Client
	cozeLoopRegistered bool
)

func initCozeLoopCallback(logger *zap.Logger) func(context.Context) {
	if !cozeLoopConfigured() {
		if logger != nil {
			logger.Info("cozeloop callback disabled; missing COZELOOP_WORKSPACE_ID or auth env")
		}
		return nil
	}

	cozeLoopMu.Lock()
	defer cozeLoopMu.Unlock()

	if cozeLoopRegistered {
		if logger != nil {
			logger.Info("cozeloop callback already registered")
		}
		return closeCozeLoopCallback
	}

	client, err := cozeloop.NewClient()
	if err != nil {
		if logger != nil {
			logger.Warn("failed to initialize cozeloop callback; tracing disabled", zap.Error(err))
		}
		return nil
	}

	handler := ccb.NewLoopHandler(client, ccb.WithLogger(cozeloop.GetLogger()))
	callbacks.AppendGlobalHandlers(handler)

	cozeLoopClient = client
	cozeLoopRegistered = true

	if logger != nil {
		logger.Info("cozeloop callback registered",
			zap.String("workspace_id", client.GetWorkspaceID()))
	}

	return closeCozeLoopCallback
}

func closeCozeLoopCallback(ctx context.Context) {
	cozeLoopMu.Lock()
	defer cozeLoopMu.Unlock()

	if !cozeLoopRegistered || cozeLoopClient == nil {
		return
	}
	cozeLoopClient.Flush(ctx)
	cozeLoopClient.Close(ctx)
	cozeLoopClient = nil
	cozeLoopRegistered = false
}

func cozeLoopConfigured() bool {
	if strings.TrimSpace(os.Getenv(cozeloop.EnvWorkspaceID)) == "" {
		return false
	}
	if strings.TrimSpace(os.Getenv(cozeloop.EnvApiToken)) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv(cozeloop.EnvJwtOAuthClientID)) != "" &&
		strings.TrimSpace(os.Getenv(cozeloop.EnvJwtOAuthPrivateKey)) != "" &&
		strings.TrimSpace(os.Getenv(cozeloop.EnvJwtOAuthPublicKeyID)) != ""
}
