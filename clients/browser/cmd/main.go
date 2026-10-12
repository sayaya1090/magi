package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sayaya1090/magi/clients/appbridge"
	"github.com/sayaya1090/magi/clients/browser"
	"github.com/sayaya1090/magi/clients/browser/headless"
	browsertool "github.com/sayaya1090/magi/internal/adapter/tool/browser"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("magi-browser", flag.ContinueOnError)
	mode := flags.String("mode", "extension", "extension or headless; test requests use headless")
	addr := flags.String("addr", "", "loopback address (extension :8737, headless :8739)")
	origins := flags.String("allow-origins", "", "comma-separated HTTP(S) origins required for headless")
	initial := flags.String("url", "", "initial headless test URL")
	actions := flags.Bool("allow-actions", true, "allow isolated test navigation/click/fill within startup origins")
	channel := flags.String("channel", "", "headless channel: chrome for installed Chrome; empty uses Playwright Chromium")
	runner := flags.String("runner", "", "path to headless runner.mjs (requires Playwright and page.js assets)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *mode != "extension" && *mode != "headless" {
		return fmt.Errorf("-mode must be extension or headless")
	}
	if *addr == "" {
		*addr = "127.0.0.1:8737"
		if *mode == "headless" {
			*addr = "127.0.0.1:8739"
		}
	}
	config := appbridge.Config{App: "browser", Addr: *addr, Token: os.Getenv("MAGI_BROWSER_TOKEN"), Binary: os.Getenv("MAGI_BINARY"), Tools: browser.Tools(), Instructions: browser.Instructions}
	var backend *headless.Backend
	if *mode == "headless" {
		allowed := []string{}
		for _, origin := range strings.Split(*origins, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				allowed = append(allowed, origin)
			}
		}
		var err error
		backend, err = headless.New(ctx, headless.Config{AllowedOrigins: allowed, InitialURL: *initial, AllowActions: *actions, RunnerPath: *runner, Channel: *channel})
		if err != nil {
			return err
		}
		defer backend.Close()
		for i := range config.Tools {
			config.Tools[i].Description = strings.ReplaceAll(config.Tools[i].Description, "after human confirmation", "within startup-authorized test origins")
		}
		config.Tools[0].Description = "List tabs in this isolated headless test profile."
		config.Call = backend.Call
		config.Release = backend.Release
		// Startup origin/action authorization is scoped by the browser role and backend.
		// Existing coding/mail/operations daemons retain their own permission policy.
		config.Permission = "allow"
		config.Instructions = "Use the browser tool in default headless mode for isolated web tests. Connect in this session, inspect a fresh page snapshot, act within allowed origins, verify outcomes and release. No user Chrome profile or account cookies are available."
	}
	s, err := appbridge.New(config)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.Config.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	s.Config.Addr = listener.Addr().String()
	life, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- s.Serve(life, listener) }()
	var primary string
	if *mode == "headless" {
		binding, e := s.Connect(ctx, "headless-tests")
		if e != nil {
			return e
		}
		primary = binding.ID
		defer s.Disconnect(context.Background(), primary)
	}
	unpublish, err := browsertool.Publish(s.Config.ConfigDir, *mode, browsertool.Runtime{URL: "http://" + s.Config.Addr, Token: s.Token})
	if err != nil {
		return err
	}
	defer unpublish()
	fmt.Println("Magi browser helper:", *mode, "http://"+s.Config.Addr)
	if *mode == "extension" {
		fmt.Println("Local connection token:", s.Token)
	} else {
		fmt.Println("Headless test origins:", strings.Join(strings.Split(*origins, ","), ", "))
	}
	select {
	case err = <-done:
		return err
	case <-ctx.Done():
		stop()
		return <-done
	}
}
