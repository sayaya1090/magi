package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/bus"
)

type earlyAdminOpts struct {
	whoami      bool
	admitFP     string
	refuseFP    string
	admitAs     string
	admitAt     string
	fleetListen string
	inviteFor   string
	joinAddr    string
	joinToken   string
	joinPin     string
	listAccessF bool
	grantWho    string
	revokeWho   string
	grantRole   string
	grantScope  string
	doTrust     bool
	doUntrust   bool
}

// runEarlyAdminCmd handles fleet identity/admission, access policy, and workspace trust commands.
// Returns (exitCode, true) if an admin command was executed, or (0, false) to continue.
func runEarlyAdminCmd(plat *platform.OS, wd string, opts earlyAdminOpts) (int, bool) {
	if opts.whoami || opts.admitFP != "" || opts.refuseFP != "" || opts.fleetListen != "" ||
		opts.inviteFor != "" || opts.joinAddr != "" {
		return runFleetCmd(fleetOpts{
			whoami: opts.whoami, admit: opts.admitFP, refuse: opts.refuseFP, as: opts.admitAs, at: opts.admitAt,
			listen: opts.fleetListen, invite: opts.inviteFor, join: opts.joinAddr,
			token: opts.joinToken, pin: opts.joinPin,
			configDir: plat.ConfigDir(), out: os.Stdout, errOut: os.Stderr,
		}), true
	}
	if opts.listAccessF || opts.grantWho != "" || opts.revokeWho != "" {
		return runAccessCmd(accessOpts{
			list: opts.listAccessF, grant: opts.grantWho, revoke: opts.revokeWho,
			role: opts.grantRole, scope: opts.grantScope,
			configDir: plat.ConfigDir(), out: os.Stdout,
		}), true
	}
	if opts.doTrust || opts.doUntrust {
		return runTrustCmd(plat.ConfigDir(), wd, opts.doUntrust, os.Stdout), true
	}
	return 0, false
}

type earlyUtilityOpts struct {
	joinTo        string
	showMembers   bool
	joinCluster   string
	relaySock     string
	fleetDoorMode bool
	mcpTo         string
	mcpAs         string
	stopDaemon    bool
	listAgents    bool
	listModels    bool
}

// runEarlyUtilityCmd handles lightweight diagnostic, query, and proxy commands that don't need the full App loop.
// Returns (exitCode, true) if a command was handled, or (0, false) to continue.
func runEarlyUtilityCmd(
	plat *platform.OS,
	wd string,
	cfg config.Config,
	store *jsonl.Store,
	llm *openai.Client,
	baseURL, apiKey string,
	opts earlyUtilityOpts,
) (int, bool) {
	if opts.joinTo != "" {
		return joinTeam(os.Stdout, plat.ConfigDir(), wd, opts.joinTo), true
	}
	if opts.showMembers {
		return exchangeMembers(os.Stdin, os.Stdout, os.Stderr, plat.ConfigDir()), true
	}
	if opts.joinCluster != "" {
		return joinTheCluster(os.Stdout, os.Stderr, plat.ConfigDir(), opts.joinCluster), true
	}
	if opts.relaySock != "" {
		return relayHere(os.Stdin, os.Stdout, os.Stderr, opts.relaySock), true
	}
	if opts.fleetDoorMode {
		return fleetDoor(os.Stdin, os.Stdout, os.Stderr, plat.ConfigDir()), true
	}
	if opts.mcpTo != "" {
		return serveMCP(opts.mcpTo, opts.mcpAs, store, plat, wd, cfg, baseURL, apiKey), true
	}
	if opts.stopDaemon {
		sock := daemon.SocketPath(plat.ConfigDir(), wd)
		cl, derr := daemon.Dial(sock)
		if derr != nil {
			fmt.Fprintln(os.Stderr, "magi:", derr)
			return 1, true
		}
		defer cl.Close()
		if err := cl.Shutdown(); err != nil {
			fmt.Fprintln(os.Stderr, "magi: stopping the daemon:", err)
			return 1, true
		}
		fmt.Fprintf(os.Stderr, "magi: asked the daemon at %s to stop\n", sock)
		return 0, true
	}
	if opts.listAgents {
		reader := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
		list, lerr := fleet.List(context.Background(), reader, plat.ConfigDir(), daemon.SocketPath(plat.ConfigDir(), wd))
		if lerr != nil {
			fmt.Fprintln(os.Stderr, "magi:", lerr)
			return 1, true
		}
		printAgents(os.Stdout, list, plat.ConfigDir())
		return 0, true
	}
	if opts.listModels {
		ids, err := llm.ListModels(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "magi: list models:", err)
			return 1, true
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return 0, true
	}
	return 0, false
}
