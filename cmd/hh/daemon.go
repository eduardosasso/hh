package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/eduardosasso/hh/internal/daemon"
)

func daemonCmd([]string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return daemon.Run(ctx)
}
