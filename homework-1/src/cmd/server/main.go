// Command server runs the banking transactions API.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/AnnieAnyWay/banking-api/internal/auth"
	"github.com/AnnieAnyWay/banking-api/internal/handler"
	"github.com/AnnieAnyWay/banking-api/internal/ratelimit"
	"github.com/AnnieAnyWay/banking-api/internal/store"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	srv := &http.Server{
		Addr: ":" + port,
		Handler: handler.New(handler.Deps{
			Store:   store.New(),
			Users:   auth.NewUsers(0),
			Tokens:  auth.NewTokens(jwtSecret(), envDuration("JWT_TTL", time.Hour)),
			Limiter: ratelimit.New(envInt("RATE_LIMIT", 100), time.Minute),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("banking API listening on http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

// jwtSecret reads JWT_SECRET (at least 32 bytes). Without it a random secret
// is generated, so tokens stop working after a restart.
func jwtSecret() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		if len(s) < 32 {
			log.Fatal("JWT_SECRET must be at least 32 characters")
		}
		return []byte(s)
	}
	log.Println("WARNING: JWT_SECRET not set, using a random secret (tokens reset on restart)")
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}
