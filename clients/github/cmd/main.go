package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sayaya1090/magi/clients/appbridge"
	gh "github.com/sayaya1090/magi/clients/github"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

func main() {
	repo := flag.String("repo", "", "absolute path to the local GitHub checkout")
	repository := flag.String("repository", "", "GitHub owner/repository matching this checkout")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	abs, e := filepath.Abs(*repo)
	target, e2 := gh.ParseTarget(*repository)
	if *repo == "" || e != nil || e2 != nil || target.Kind != "" {
		fmt.Fprintln(os.Stderr, "-repo 로컬경로와 -repository 소유자/저장소를 지정해 주세요.")
		os.Exit(2)
	}
	cmd := exec.Command("git", "-C", abs, "remote", "get-url", "origin")
	raw, e := cmd.Output()
	remote := strings.TrimSpace(string(raw))
	want := *repository
	if e != nil || remote != "https://github.com/"+want+".git" && remote != "https://github.com/"+want && remote != "git@github.com:"+want+".git" {
		fmt.Fprintln(os.Stderr, "로컬 저장소 origin과 GitHub 대상이 일치해야 합니다.")
		os.Exit(2)
	}
	dir := os.Getenv("MAGI_CONFIG_DIR")
	if dir == "" {
		base, e := os.UserConfigDir()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		dir = filepath.Join(base, "magi")
	}
	api := gh.API{Token: os.Getenv("GITHUB_TOKEN")}
	s, err := appbridge.New(appbridge.Config{App: "github", Addr: "127.0.0.1:8737", ConfigDir: dir, Token: os.Getenv("MAGI_GITHUB_TOKEN"), Instructions: gh.Instructions, Tools: gh.Tools(), Call: api.Call, Socket: func(ctx context.Context, key string) (string, error) {
		t, e := gh.ParseTarget(key)
		if e != nil || t.Owner+"/"+t.Repo != want {
			return "", fmt.Errorf("이 헬퍼는 %s 저장소에만 연결합니다.", want)
		}
		sock := daemon.SocketPath(dir, abs)
		cl, e := daemon.DialWithin(sock, time.Second, time.Second)
		if e == nil {
			cl.Close()
			return sock, nil
		}
		bin := os.Getenv("MAGI_BINARY")
		if bin == "" {
			bin = "magi"
		}
		cmd := exec.CommandContext(ctx, bin, "--daemon", "--detach", "--no-update-check", "--permission", "ask", "--client-role", "github")
		cmd.Dir = abs
		cmd.Env = appbridge.CompanionEnv(os.Environ(), dir)
		if e = cmd.Run(); e != nil {
			return "", fmt.Errorf("코딩 컴패니언을 시작하지 못했습니다.")
		}
		return sock, nil
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Magi GitHub: 127.0.0.1:8737 · " + want)
	fmt.Println("Local UI bearer token:", s.Token)
	if err = s.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
