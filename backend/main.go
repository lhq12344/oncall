package main

import (
	"log"

	"go_agent/internal/bootstrap"
)

// main is a compatibility entrypoint. The canonical server entrypoint lives in
// cmd/oncall and delegates to the same application runtime.
func main() {
	if err := bootstrap.RunHTTPServer(); err != nil {
		log.Fatalf("oncall server stopped: %v", err)
	}
}
