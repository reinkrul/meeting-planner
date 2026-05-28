package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/reinkrul/meeting-planner/internal/booking"
	googleprov "github.com/reinkrul/meeting-planner/internal/calendar/google"
	"github.com/reinkrul/meeting-planner/internal/config"
	"github.com/reinkrul/meeting-planner/internal/notify"
	"github.com/reinkrul/meeting-planner/internal/server"
	"github.com/reinkrul/meeting-planner/internal/store"
)

const version = "0.1.0"

func main() {
	configFile := flag.String("config", "", "path to config YAML (or set MP_CONFIG_FILE / MP_CONFIG)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	sub := "serve"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	cfg, err := config.Load(config.Options{ConfigFile: *configFile})
	if err != nil {
		fatal("config: %v", err)
	}
	st, err := store.Open(cfg.DataDir, cfg.Server.CapabilityToken)
	if err != nil {
		fatal("state: %v", err)
	}

	switch sub {
	case "serve":
		runServe(cfg, st)
	case "reauth":
		runReauth(cfg, st, args)
	case "rotate-capability":
		runRotateCapability(st, cfg)
	case "print-urls":
		runPrintURLs(cfg, st)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `meeting-planner v%s

usage:
  meeting-planner [-config FILE] [SUBCOMMAND] [ARGS]

subcommands:
  serve                 start the HTTP server (default)
  reauth <calendar-id>  drop OAuth tokens for one calendar so /admin re-enables
  rotate-capability     mint a new capability token (invalidates the old URL)
  print-urls            print the capability URL (and admin URL, if enabled)

flags:
`, version)
	flag.PrintDefaults()
}

func runServe(cfg config.Config, st *store.State) {
	providers, err := booking.BuildProviders(cfg, st)
	if err != nil {
		fatal("providers: %v", err)
	}
	googleProviders := map[string]*googleprov.Provider{}
	for _, c := range cfg.Calendars {
		if c.Provider == "google" {
			googleProviders[c.ID] = providers[c.ID].(*googleprov.Provider)
		}
	}
	notifier, err := buildNotifier(cfg)
	if err != nil {
		fatal("notifier: %v", err)
	}
	bs := booking.NewService(cfg, st, providers, notifier)
	srv, err := server.New(cfg, st, bs, googleProviders)
	if err != nil {
		fatal("server: %v", err)
	}

	printStartupBanner(cfg, st)

	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func runReauth(cfg config.Config, st *store.State, args []string) {
	if len(args) < 1 {
		fatal("reauth: <calendar-id> required")
	}
	id := args[0]
	found := false
	for _, c := range cfg.Calendars {
		if c.ID == id {
			found = true
			if !c.RequiresOAuth() {
				fatal("reauth: calendar %q does not use OAuth", id)
			}
			break
		}
	}
	if !found {
		fatal("reauth: no calendar with id %q", id)
	}
	if err := st.DropOAuthToken(id); err != nil {
		fatal("reauth: %v", err)
	}
	fmt.Printf("OAuth tokens dropped for %q. /admin will be enabled on next request.\n", id)
}

func runRotateCapability(st *store.State, cfg config.Config) {
	tok, err := st.RotateCapabilityToken()
	if err != nil {
		fatal("rotate-capability: %v", err)
	}
	fmt.Printf("New booking link: %s\n", capabilityURL(cfg, tok))
}

func runPrintURLs(cfg config.Config, st *store.State) {
	fmt.Printf("Booking link: %s\n", capabilityURL(cfg, st.CapabilityToken()))
	pending := st.NeedsOAuth(cfg.Calendars)
	if len(pending) == 0 {
		fmt.Println("Admin:          disabled (all calendars connected)")
		return
	}
	fmt.Printf("Admin:          ENABLED — %s (calendars needing OAuth: %s)\n",
		strings.TrimRight(cfg.Server.PublicBaseURL, "/")+"/admin/",
		strings.Join(pending, ", "),
	)
}

func capabilityURL(cfg config.Config, tok string) string {
	return strings.TrimRight(cfg.Server.PublicBaseURL, "/") + "/c/" + tok
}

func printStartupBanner(cfg config.Config, st *store.State) {
	log.Printf("meeting-planner v%s", version)
	log.Printf("  Listening:      %s", cfg.Server.Listen)
	log.Printf("  Public base:    %s", cfg.Server.PublicBaseURL)
	log.Printf("  Booking link: %s", capabilityURL(cfg, st.CapabilityToken()))
	var calStatus []string
	for _, c := range cfg.Calendars {
		status := c.Provider
		if c.RequiresOAuth() {
			if st.OAuthToken(c.ID) == nil {
				status += ", NOT CONNECTED"
			} else {
				status += ", connected"
			}
		}
		calStatus = append(calStatus, fmt.Sprintf("%s (%s)", c.ID, status))
	}
	log.Printf("  Calendars:      %s", strings.Join(calStatus, ", "))
	pending := st.NeedsOAuth(cfg.Calendars)
	if len(pending) == 0 {
		log.Printf("  Admin:          disabled (all calendars connected)")
	} else {
		log.Printf("  Admin:          ENABLED — %s/admin/ (%d calendar(s) need OAuth)",
			strings.TrimRight(cfg.Server.PublicBaseURL, "/"), len(pending))
	}
}

func buildNotifier(cfg config.Config) (notify.Notifier, error) {
	if !cfg.Notifications.SMTP.Enabled {
		return notify.Disabled{}, nil
	}
	return notify.NewSMTP(cfg.Notifications.SMTP)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
