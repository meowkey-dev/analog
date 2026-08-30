// Command analog-server runs the API.
//
// Serving is the default. `seed` and `token` are operator commands that work on the
// same data directory without a server running, which is why they live on this
// binary rather than on the client CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/internal/api"
	"github.com/meowkey-dev/analog/internal/auth"
	"github.com/meowkey-dev/analog/internal/config"
	"github.com/meowkey-dev/analog/internal/store"
	"github.com/meowkey-dev/analog/internal/tokencli"
	"github.com/meowkey-dev/analog/internal/web"
)

func main() {
	if err := root().Execute(); err != nil {
		// cobra has already printed the message.
		os.Exit(1)
	}
}

func root() *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:          "analog-server",
		Short:        "Run the Analog API",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return serve(host, port)
		},
	}
	cmd.Flags().StringVar(&host, "host", config.Host(),
		"0.0.0.0 to accept connections from other machines")
	cmd.Flags().IntVar(&port, "port", config.Port(), "TCP port to listen on")
	cmd.AddCommand(seedCmd(), tokencli.Command())
	return cmd
}

func serve(host string, port int) error {
	tokens := auth.NewStore(config.AuthPath())
	// Checked before the listener binds, rather than after, so an unauthenticated
	// server on a network never accepts a single request.
	if err := auth.RequireAuthForHost(host, tokens); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	st, err := store.Open(config.DBPath(), config.MediaDir())
	if err != nil {
		return err
	}
	defer st.Close()

	server := api.New(st, tokens, web.Dist())
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	where := "REACHABLE FROM THE NETWORK"
	if auth.IsLoopback(host) {
		where = "loopback only"
	}
	authState := "off — no tokens configured"
	if tokens.Enabled() {
		authState = "per-actor tokens"
	}
	fmt.Printf("analog on http://%s  (%s)\n", addr, where)
	fmt.Printf("  auth: %s\n", authState)
	fmt.Printf("  data: %s\n", config.DBPath())

	httpServer := &http.Server{
		Handler: server,
		// No write deadline: an SSE subscriber holds its response open for as long
		// as the human keeps the tab.
		ReadHeaderTimeout: 10 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		return nil
	}
}
