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
	"github.com/gerp93/KVGuzzler/server/internal/sim"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	listen := flag.String("listen", ":8080", "address to serve the API on")
	data := flag.String("data", "data", "folder for house.db")
	simulate := flag.Bool("sim", false, "poll a made-up house instead of real devices (no hardware needed)")
	tz := flag.String("tz", "", `IANA time zone for "day" buckets and bill dates, e.g. America/Chicago (default: this machine's)`)
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
	if simulate {
		if err := col.Add(ctx, sim.New()); err != nil {
			return err
		}
		log.Print("polling a simulated house: nothing here is a measurement")
	} else {
		log.Print("no devices configured yet (real Refoss support is not built); use -sim to try the server")
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           (&api.Server{Store: st, Collector: col, Loc: loc, Version: version}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
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
