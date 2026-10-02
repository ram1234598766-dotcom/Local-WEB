// Command rendezvous runs a reference rendezvous server.
//
// The rendezvous client was already implemented and already flagged
// (-rendezvous <url>), but there was no server to point it at. Two nodes on
// different networks could find each other only if the operator already ran
// rendezvous infrastructure of their own. This is that server, so crossing the
// internet is a command rather than a project.
//
// What it is: an address book. Nodes tell it where they can be reached, and it
// tells other nodes. It stores nothing but a node id, a public key, a display
// name and a short list of addresses.
//
// What it is not: a relay. No traffic passes through it. Terminating media or
// encrypting anything on someone else's behalf is not something an operator
// should be signed up for by installing this, and there is no code path that
// would let it.
//
// It is unauthenticated by design, because a rendezvous server is discovered by
// address rather than by account, and because requiring registration would make
// bootstrapping the network a chicken-and-egg problem. That makes the write path
// public, so it is proof-of-work gated and every input is bounded. See
// pkg/federation/rendezvous_server.go for the limits and why each exists.
//
// Example:
//
//	go run ./cmd/rendezvous --addr 0.0.0.0:8477 --difficulty 10
//	go run ./cmd/node -rendezvous https://rendezvous.example.net
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/federation"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8477", "listen address")
	difficulty := flag.Int("difficulty", dht.MinPoWDifficulty,
		"proof-of-work difficulty required to register (higher is harder and slower to attack)")
	shutdownGrace := flag.Duration("shutdown-grace", 10*time.Second,
		"how long to wait for in-flight requests on shutdown")
	flag.Parse()

	store := federation.NewMemoryStore()
	srv := federation.NewHTTPHandler(store, federation.WithDifficulty(*difficulty))
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		// A registration is small and PoW is solved by the client, so these bounds
		// only exist to stop a slow client from holding a connection open.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("rendezvous: listening on %s, proof-of-work difficulty %d", *addr, *difficulty)
	log.Printf("rendezvous: this server is an address book only; no traffic is relayed through it")

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("rendezvous: serve: %v", err)
	case sig := <-sigCh:
		log.Printf("rendezvous: %s received, shutting down with %d peers registered",
			sig, len(store.All()))
	}

	// Shut down cleanly so a restart does not drop a registration mid-flight. The
	// store is in memory either way, so this is about not cutting a client's
	// request in half.
	ctx, cancel := context.WithTimeout(context.Background(), *shutdownGrace)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "rendezvous: shutdown: %v\n", err)
	}
	log.Printf("rendezvous: stopped")
}
