// housed is the house server: it polls the energy monitors and plugs, keeps the history and serves the API.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // so -tz works on a machine with no zone database

	"github.com/gerp93/KVGuzzler/server/internal/api"
	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/devices"
	"github.com/gerp93/KVGuzzler/server/internal/sim"
	"github.com/gerp93/KVGuzzler/server/internal/store"
	"github.com/gerp93/KVGuzzler/server/internal/webui"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

// envOr is the value of an environment variable, or def when it is unset or empty. It lets a service be configured from an
// environment file on the machine (HOUSED_LISTEN, HOUSED_DATA, HOUSED_TZ) instead of from command-line flags in a unit file
// that is kept in the repository. A flag given on the command line still wins.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listen := flag.String("listen", envOr("HOUSED_LISTEN", ":8080"), "address to serve the API on (env HOUSED_LISTEN)")
	data := flag.String("data", envOr("HOUSED_DATA", "data"), "folder for house.db (env HOUSED_DATA)")
	simulate := flag.Bool("sim", false, "poll a made-up house instead of real devices (no hardware needed)")
	tz := flag.String("tz", envOr("HOUSED_TZ", ""), `IANA time zone for "day" buckets and bill dates, e.g. America/Chicago (env HOUSED_TZ; default: this machine's)`)
	flag.Parse()

	loc := time.Local
	if *tz != "" {
		var err error
		if loc, err = time.LoadLocation(*tz); err != nil {
			log.Fatalf("-tz %q: %v", *tz, err)
		}
	}
	if err := run(*listen, *data, *simulate, loc); err != nil {
		log.Fatal(err)
	}
}

func run(listen, dataDir string, simulate bool, loc *time.Location) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "house.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	col := collector.New(st, time.Second)
	mgr := &devices.Manager{Store: st, Collector: col}
	if err := mgr.Restore(ctx); err != nil {
		return err
	}
	if simulate {
		if err := col.Add(ctx, sim.New()); err != nil {
			return err
		}
		log.Print("polling a simulated house: nothing here is a measurement")
	} else if len(col.Devices()) == 0 {
		log.Print("no devices yet: add a plug in the dashboard's Settings (Refoss monitors are not supported yet), or run with -sim")
	}

	root := http.NewServeMux()
	root.Handle("/api/", (&api.Server{Store: st, Collector: col, Devices: mgr, Loc: loc, Version: version}).Handler())
	root.Handle("/", webui.Handler())
	srv := &http.Server{Addr: listen, Handler: root, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() { col.Run(ctx); close(done) }()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("housed %s listening on %s", version, listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}
