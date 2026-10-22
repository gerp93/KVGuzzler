package refoss

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The device-info shape is the one a real EM06P (firmware 2.1.13) was reported to answer with.
const deviceInfoBody = `{"name":"Refoss Smart Energy Monitor","model":"em06p","dev_id":"refoss-em06p-c4e7ae","api_ver":"1.0","fw_ver":"2.1.13","hw_ver":"2.0.0","auth_en":false}`

func TestDeviceInfo(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(deviceInfoBody))
	}))
	defer srv.Close()

	info, err := newWithBase(srv.URL, srv.Client()).DeviceInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/rpc/Refoss.DeviceInfo.Get" {
		t.Errorf("request was %s %s", gotMethod, gotPath)
	}
	if info.Model != "em06p" || info.FwVer != "2.1.13" || info.AuthEn {
		t.Errorf("parsed %+v", info)
	}
}

func TestAuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", `Digest qop="auth", realm="x", nonce="1", algorithm=SHA-256`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newWithBase(srv.URL, srv.Client()).StatusRaw(context.Background())
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("err = %v, want ErrAuthRequired", err)
	}
}

func TestOnlyAllowedMethodsAreCalled(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	c := newWithBase(srv.URL, srv.Client())
	for _, m := range []string{"Em.Config.Set", "Switch.Set", "Refoss.Reboot", "../x"} {
		if _, err := c.get(context.Background(), m); err == nil {
			t.Errorf("get(%q) succeeded, want a refusal", m)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests reached the device, want none", hits.Load())
	}
}

func TestNewRefusesNonPlainHost(t *testing.T) {
	for _, h := range []string{"http://10.0.0.5", "10.0.0.5:80", "10.0.0.5/rpc", ""} {
		if _, err := New(h, nil); err == nil {
			t.Errorf("New(%q) succeeded, want an error", h)
		}
	}
	if _, err := New("10.0.0.5", nil); err != nil {
		t.Errorf("New(10.0.0.5) = %v", err)
	}
}
