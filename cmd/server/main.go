// Command server runs the PR Cost API server.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/codeGROOVE-dev/prcost/internal/server"
)

const (
	defaultPort       = "8080"
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 5 * time.Minute // Long timeout for SSE streams
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 1 << 20 // 1MB
)

// Build variables - set by ldflags.
var (
	GitCommit = "unknown"
	GitBranch = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Create root context
	ctx := context.Background()

	// Set up logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Log startup information
	logger.InfoContext(ctx, "starting server",
		"commit", GitCommit,
		"branch", GitBranch,
		"built", BuildTime,
		"go", runtime.Version(),
		"pid", os.Getpid())

	// Parse flags
	var (
		port        = flag.String("port", "", "Port to run the server on")
		version     = flag.Bool("version", false, "Print version and exit")
		corsOrigins = flag.String("cors-origins",
			"https://github.com,https://api.github.com",
			"Comma-separated list of allowed CORS origins (supports *.domain.com wildcards)")
		allowAllCors   = flag.Bool("allow-all-cors", false, "Allow all CORS origins (use only for development)")
		rateLimit      = flag.Int("rate-limit", 100, "Requests per second rate limit")
		rateBurst      = flag.Int("rate-burst", 100, "Rate limit burst size")
		validateTokens = flag.Bool("validate-tokens", false, "Validate GitHub tokens server-side")
		githubAppID    = flag.String("github-app-id", "", "GitHub App ID for token validation")
		githubAppKey   = flag.String("github-app-key-file", "", "Path to GitHub App private key file")
		dataSource     = flag.String("data-source", "prx", "Data source for PR data (prx or turnserver)")
	)
	flag.Parse()

	if *version {
		logger.InfoContext(ctx, "prcost-server version",
			"commit", GitCommit,
			"branch", GitBranch,
			"built", BuildTime,
			"go", runtime.Version())
		os.Exit(0)
	}

	// Determine port
	serverPort := *port
	if serverPort == "" {
		serverPort = os.Getenv("PORT")
	}
	if serverPort == "" {
		serverPort = defaultPort
	}

	// Determine data source (environment variable overrides flag default)
	dataSourceValue := *dataSource
	if envDataSource := os.Getenv("DATA_SOURCE"); envDataSource != "" {
		dataSourceValue = envDataSource
	}

	// Check R2R_CALLOUT environment variable
	r2rCallout := os.Getenv("R2R_CALLOUT") == "1"

	// Create server
	prcostServer := server.New()
	prcostServer.SetCommit(GitCommit)
	prcostServer.SetCORSConfig(*corsOrigins, *allowAllCors)
	prcostServer.SetRateLimit(*rateLimit, *rateBurst)
	prcostServer.SetDataSource(dataSourceValue)
	prcostServer.SetR2RCallout(r2rCallout)
	if *validateTokens {
		if *githubAppID == "" || *githubAppKey == "" {
			logger.ErrorContext(ctx, "github app ID and key file are required when token validation is enabled")
			os.Exit(1)
		}
		if err := prcostServer.SetTokenValidation(*githubAppID, *githubAppKey); err != nil {
			logger.ErrorContext(ctx, "failed to configure token validation", "error", err)
			os.Exit(1)
		}
	}
	srv := &http.Server{
		Addr:              ":" + serverPort,
		Handler:           prcostServer,
		ReadTimeout:       readHeaderTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "server listening", "port", serverPort)
		serverErrors <- srv.ListenAndServe()
	}()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "server error", "error", err)
			os.Exit(1)
		}
	case sig := <-sigChan:
		logger.InfoContext(ctx, "received signal", "signal", sig)

		// Graceful shutdown
		logger.InfoContext(ctx, "starting graceful shutdown")

		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)

		// Shutdown application components
		prcostServer.Shutdown()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			cancel()
			logger.WarnContext(ctx, "graceful shutdown failed", "error", err)
			// Force close
			if err := srv.Close(); err != nil {
				logger.ErrorContext(ctx, "server close error", "error", err)
				os.Exit(1)
			}
		} else {
			cancel()
		}
	}

	logger.InfoContext(ctx, "server stopped")
}
