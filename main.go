package main

import (
	"log"
	"hypnotz/internal/server"
)

func main() {
	app := server.NewServer()

	log.Println("Projection Engine starting on :8080")
	if err := app.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
