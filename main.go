package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	// Embed the timezone database so TZ works in minimal containers.
	_ "time/tzdata"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	store, err := LoadStore(cfg.StatePath)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := NewBot(NewAPI(cfg.Token), cfg, store).Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Print("stopped")
}
