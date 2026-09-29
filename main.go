// Command time-tracker is Time Tracker as a Google Workspace add-on over HTTP, for
// Google Calendar: Workspace POSTs each event as JSON, and the service answers with
// the card to draw. It writes entries to the user's calendar with the user's own
// token, and asks Gemini to turn a typed note into an entry.
//
// Env:
//
//	PORT            the port to listen on (default 8080)
//	K_SERVICE       set by Cloud Run; without it the service binds 127.0.0.1 only
//	GEMINI_API_KEY  the Gemini key, from Secret Manager on Cloud Run; unset, the form
//	                has no Describe it section
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/duizendstra-com/time-tracker/addon"
	"github.com/duizendstra-com/time-tracker/remote"
)

func main() {
	onCloudRun := os.Getenv("K_SERVICE") != ""
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if onCloudRun {
		// Cloud Logging reads severity and message.
		log = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				switch {
				case len(groups) > 0:
				case a.Key == slog.LevelKey:
					a.Key = "severity"
				case a.Key == slog.MessageKey:
					a.Key = "message"
				}
				return a
			},
		}))
	}
	slog.SetDefault(log)

	svc := &addon.Service{
		Store:  func(token string) addon.Store { return remote.NewCalendar(nil, token) },
		Sheets: func(token string) addon.Sheets { return remote.NewSheet(nil, token) },
		Log:    log,
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		svc.Gemini = remote.NewGemini(nil, key)
	} else {
		log.Warn("GEMINI_API_KEY is not set: the form has no Describe it section")
	}
	if onCloudRun {
		svc.Project = remote.ProjectID(context.Background())
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	// Loopback unless Cloud Run runs us: a local run is never reachable from the network.
	addr := "127.0.0.1:" + port
	if onCloudRun {
		addr = ":" + port
	}
	mux := http.NewServeMux()
	mux.Handle("/", svc)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Workspace gives up on a card action long before this.
		WriteTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	log.Info("time-tracker listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
