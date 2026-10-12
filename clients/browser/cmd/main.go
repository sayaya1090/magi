package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sayaya1090/magi/clients/appbridge"
	"github.com/sayaya1090/magi/clients/browser"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, err := appbridge.New(appbridge.Config{App: "browser", Addr: "127.0.0.1:8737", Token: os.Getenv("MAGI_BROWSER_TOKEN"), Binary: os.Getenv("MAGI_BINARY"), Tools: browser.Tools(), Instructions: browser.Instructions})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Magi Chrome helper: http://127.0.0.1:8737")
	fmt.Println("Local connection token:", s.Token)
	if err = s.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
