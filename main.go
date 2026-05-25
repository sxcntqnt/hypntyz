package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hypnotz/internal/server"
)

func main() {
	app := server.NewServer()

	go func() {
		app.StartTickLoop()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down Projection Engine...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := app.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	if err := app.Run(":8901"); err != nil {
		log.Fatal(err)
	}
}
