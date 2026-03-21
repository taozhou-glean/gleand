package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/nickolasclarke/gleand/internal/config"
	"github.com/nickolasclarke/gleand/internal/daemon"
)

var version = "dev"

func main() {
	var (
		backend     = flag.String("backend", "", "Glean backend URL (overrides config)")
		authToken   = flag.String("token", "", "OAuth bearer token (overrides config)")
		scParams    = flag.String("sc", "", "Additional sc params for the chat API")
		interactive = flag.Bool("interactive", false, "Run in interactive REPL mode for E2E testing")
		chatID      = flag.String("chat-id", "", "Resume an existing chat session by ID")
		debug       = flag.Bool("debug", false, "Enable debug logging with colored output")
		showVer     = flag.Bool("version", false, "Print version and exit")
		listTools   = flag.Bool("list-tools", false, "List registered tools and exit")
		configCmd   = flag.Bool("config-path", false, "Print config file path and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("gleand", version)
		os.Exit(0)
	}

	if *configCmd {
		fmt.Println(config.ConfigPath())
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %s\n", err)
		os.Exit(1)
	}

	if *backend != "" {
		cfg.Backend = *backend
	}
	if *authToken != "" {
		cfg.AuthToken = *authToken
	}

	if *scParams != "" {
		cfg.ScParams = *scParams
	} else if len(cfg.ScParams) == 0 {
		cfg.ScParams = "co.enable_client_tools=1,db.py_agents_service_name=pyagents-glean-exp-129,co.lo.enable_ai_coding_assistant_native_tool=false"
	}
	cfg.Debug = *debug

	if envToken := os.Getenv("GLEAN_AUTH_TOKEN"); envToken != "" && cfg.AuthToken == "" {
		cfg.AuthToken = envToken
	}

	var logger *slog.Logger
	if cfg.Debug {
		logger = slog.New(daemon.NewDebugLogHandler(os.Stderr))
	} else {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	if *listTools {
		d := daemon.New(cfg, logger)
		for _, def := range d.ToolDefinitions() {
			fmt.Printf("%-30s %s\n", def.ToolID, def.Description)
		}
		os.Exit(0)
	}

	d := daemon.New(cfg, logger)

	var runErr error
	if *interactive || *chatID != "" {
		runErr = d.RunInteractiveWithChatID(context.Background(), *chatID)
	} else {
		runErr = d.Run(context.Background())
	}

	if runErr != nil {
		logger.Error("daemon exited with error", "error", runErr)
		os.Exit(1)
	}
}
