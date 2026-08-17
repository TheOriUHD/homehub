package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"homehub/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "homehub:", err)
		os.Exit(1)
	}
}
