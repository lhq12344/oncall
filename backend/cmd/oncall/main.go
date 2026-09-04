package main

import (
	"log"

	"go_agent/internal/bootstrap"
)

func main() {
	if err := bootstrap.RunHTTPServer(); err != nil {
		log.Fatalf("oncall server stopped: %v", err)
	}
}
