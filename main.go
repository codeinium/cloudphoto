package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdout, os.Getenv, newArchive); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
