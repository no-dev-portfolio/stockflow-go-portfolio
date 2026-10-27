package main

import (
	"context"
	"errors"
	"example.com/stockflow/internal/httpapi"
	"example.com/stockflow/internal/store"
	"example.com/stockflow/internal/worker"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "health" {
		if err := checkHealth(os.Getenv("LISTEN_ADDR")); err != nil {
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("shutdown_with_error", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	if os.Getenv("DEMO_MODE") != "true" {
		return errors.New("this distribution is a local synthetic-data demo; set DEMO_MODE=true")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := store.Open(startup, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Pool.Close()
	if err = db.Migrate(startup); err != nil {
		return err
	}
	if err = db.Seed(startup); err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return nil
	}
	interval := time.Second
	count := 2
	failures := 0
	if v := os.Getenv("DEMO_DELIVERY_FAILURES"); v != "" {
		failures, err = strconv.Atoi(v)
		if err != nil || failures < 0 || failures > 5 {
			return errors.New("DEMO_DELIVERY_FAILURES must be 0..5")
		}
	}
	workerCtx, workerStop := context.WithCancel(context.Background())
	defer workerStop()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Worker{Store: db, Logger: logger, Count: count, Interval: interval, SimulateFailures: failures}.Run(workerCtx)
	}()
	api := &httpapi.API{Store: db, Logger: logger, Credentials: httpapi.DemoCredentials()}
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	failed := make(chan error, 1)
	go func() {
		logger.Info("server_started", "address", address, "demo", true)
		failed <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			workerStop()
			<-workerDone
			return err
		}
	}
	shutdown, done := context.WithTimeout(context.Background(), 12*time.Second)
	defer done()
	err = server.Shutdown(shutdown)
	workerStop()
	<-workerDone
	logger.Info("shutdown_complete")
	return err
}

// Connect to loopback when the server listens on an unspecified interface.
func healthURL(address string) (string, error) {
	if address == "" {
		address = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if port == "" {
		return "", errors.New("missing listen port")
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}
func checkHealth(address string) error {
	endpoint, err := healthURL(address)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("server is not ready")
	}
	return nil
}
