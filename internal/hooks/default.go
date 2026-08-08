package hooks

import "sync"

var (
	defaultEngineMu sync.RWMutex
	defaultEngine   = NewDisabledEngine()
)

func SetDefaultEngine(engine *Engine) {
	defaultEngineMu.Lock()
	defer defaultEngineMu.Unlock()
	if engine == nil {
		defaultEngine = NewDisabledEngine()
		return
	}
	defaultEngine = engine
}

func DefaultEngine() *Engine {
	defaultEngineMu.RLock()
	defer defaultEngineMu.RUnlock()
	return defaultEngine
}
