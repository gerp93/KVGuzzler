package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/refoss"
)

func TestProbePrintsBothAnswersAndOnlyReads(t *testing.T) {
	var paths []string
	var nonGet atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet {
			nonGet.Add(1)
		}
		switch r.URL.Path {
		case "/rpc/Refoss.DeviceInfo.Get":
			_, _ = w.Write([]byte(`{"name":"Refoss Smart Energy Monitor","model":"em16p","dev_id":"x","api_ver":"1.0","fw_ver":"2.1.13","hw_ver":"2.0.0","auth_en":false}`))
		case "/rpc/Refoss.Status.Get":
			_, _ = w.Write([]byte(`{"mystery":{"unknown":[1,2,3]}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := probe(context.Background(), refoss.NewWithBase(srv.URL, srv.Client()), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Refoss.DeviceInfo.Get", `"model": "em16p"`, "Refoss.Status.Get (raw answer)", `"mystery"`, "status-em16p.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if len(paths) != 2 || nonGet.Load() != 0 {
		t.Errorf("paths %v, %d non-GET requests: it must make exactly the two read calls", paths, nonGet.Load())
	}
}

func TestProbeSaysWhenTheMonitorWantsALogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rpc/Refoss.DeviceInfo.Get" {
			_, _ = w.Write([]byte(`{"model":"em06p","auth_en":true}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := probe(context.Background(), refoss.NewWithBase(srv.URL, srv.Client()), &out)
	if err == nil || !strings.Contains(err.Error(), "local authentication") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), "authentication is ON") {
		t.Errorf("output = %q", out.String())
	}
}
