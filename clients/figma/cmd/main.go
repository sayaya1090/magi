package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sayaya1090/magi/clients/appbridge"
	figma "github.com/sayaya1090/magi/clients/figma"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	bridge, err := appbridge.New(appbridge.Config{App: "figma", Addr: "127.0.0.1:8734", Token: os.Getenv("MAGI_FIGMA_TOKEN"), ConfigDir: os.Getenv("MAGI_CONFIG_DIR"), Binary: os.Getenv("MAGI_BINARY"), Tools: figma.Tools(), Instructions: figma.Instructions})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Magi Figma helper: http://127.0.0.1:8734")
	fmt.Println("Local connection token:", bridge.Token)
	if err = bridge.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
