package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/raimbekalish/faultline/internal/buildinfo"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raimbekalish/faultline/internal/dashboard"
	"github.com/raimbekalish/faultline/internal/history"
)

func serve(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && args[0] == "--version" {
		return buildinfo.Write(out)
	}
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	flags.SetOutput(out)
	db := flags.String("db", "artifacts/faultline.sqlite", "existing local history database")
	assets := flags.String("assets", "web/dist", "built dashboard assets")
	address := flags.String("listen", "127.0.0.1:0", "IPv4 loopback address; ephemeral port by default")
	ttl := flags.Duration("ttl", time.Hour, "server lifetime (1s–8h)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || host != "127.0.0.1" {
		return errors.New("listen must be 127.0.0.1:port")
	}
	if *ttl < time.Second || *ttl > 8*time.Hour {
		return errors.New("ttl must be 1s–8h")
	}
	if info, err := os.Stat(*assets + "/index.html"); err != nil || !info.Mode().IsRegular() {
		return errors.New("dashboard assets missing; build web first")
	}
	openCtx, cancel := context.WithTimeout(ctx, history.OperationTimeout)
	store, err := history.OpenExisting(openCtx, *db)
	cancel()
	if err != nil {
		return fmt.Errorf("open existing history (run an experiment first): %w", err)
	}
	defer store.Close()
	listener, err := net.Listen("tcp4", *address)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: dashboard.Handler(store, os.DirFS(*assets), listener.Addr().String()), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(out, "http: ", 0)}
	lifetime, stop := context.WithTimeout(ctx, *ttl)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() {
		<-lifetime.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdown)
	}()
	fmt.Fprintf(out, "Faultline dashboard: http://%s (read-only, expires in %s)\n", listener.Addr(), *ttl)
	err = server.Serve(listener)
	stop()
	shutdownErr := <-shutdownDone
	if shutdownErr != nil {
		_ = server.Close()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return shutdownErr
	}
	return errors.Join(err, shutdownErr)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
