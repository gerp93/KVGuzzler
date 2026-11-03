package shelly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/source"
)

// A real Sys.GetStatus answer from a Plug US Gen4 (the address and the MAC taken out).
const sysAnswer = `{"restart_required":false,"time":"20:04","unixtime":1793671499,"last_sync_ts":1793670531,"uptime":19040,"ram_size":340928,"ram_free":148804,"ram_min_free":127784,"fs_size":917504,"fs_free":471040,"cfg_rev":4,"kvs_rev":0,"schedule_rev":0,"webhook_rev":0,"btrelay_rev":0,"available_updates":{},"reset_reason":1,"utc_offset":-21600}`

func TestParsesHowLongThePlugHasBeenUpAndWhyItStarted(t *testing.T) {
	sys, err := ParseSys([]byte(sysAnswer))
	if err != nil || sys.Uptime != 19040*time.Second || sys.ResetReason == nil || *sys.ResetReason != 1 {
		t.Fatalf("%+v, %v", sys, err)
	}
	// No reason is not an error: the uptime is still good.
	sys, err = ParseSys([]byte(`{"uptime":60}`))
	if err != nil || sys.Uptime != time.Minute || sys.ResetReason != nil {
		t.Errorf("without a reason: %+v, %v", sys, err)
	}
	for _, body := range []string{``, `[]`, `{}`, `{"uptime":"60"}`, `{"uptime":-1}`, `{"uptime":null}`} {
		if _, err := ParseSys([]byte(body)); err == nil {
			t.Errorf("%q was taken for an uptime", body)
		}
	}
}

func TestTheStartIsFoundWithOneReadAndSaysWhetherPowerCameBack(t *testing.T) {
	var method, path string
	var reason string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		body := `{"uptime":3600,"reset_reason":` + reason + `}`
		if reason == "" {
			body = `{"uptime":3600}`
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	src := NewSourceWithBase(srv.URL, "192.168.1.50", "PC", model.ScopePlug, srv.Client())
	var _ source.Booter = src

	for r, want := range map[string]source.BootCause{"1": source.BootPower, "9": source.BootPower, "3": source.BootOther, "": source.BootUnknown} {
		reason = r
		before := time.Now()
		b, err := src.Boot(context.Background())
		if err != nil {
			t.Fatalf("reason %q: %v", reason, err)
		}
		if b.Cause != want {
			t.Errorf("reason %q: cause %v, want %v", reason, b.Cause, want)
		}
		if got := before.Sub(b.At); got < 3599*time.Second || got > 3601*time.Second {
			t.Errorf("reason %q: started %v before the question, want an hour", reason, got)
		}
		if b.DeviceID != "shelly-192.168.1.50" {
			t.Errorf("device %q", b.DeviceID)
		}
	}
	if method != http.MethodGet || path != "/rpc/Sys.GetStatus" {
		t.Errorf("asked with %s %s, want a plain GET of Sys.GetStatus", method, path)
	}
}
