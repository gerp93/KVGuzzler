// Package refoss reads a Refoss energy monitor over its local HTTP API (`GET http://<host>/rpc/<Method>`).
//
// It is read-only by construction: get refuses any method that is not on the allowlist, so no `*.Set` call can be
// made from here. Local authentication is expected to be off on the device (a 401 is reported as such).
//
// Only DeviceInfo.Get is parsed. The shape of Status.Get (the per-channel watts, volts, amps and kWh) is not confirmed
// against a real device yet, so StatusRaw returns the body untouched and nothing here guesses its fields.
package refoss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/hostcheck"
)

const (
	MethodDeviceInfo = "Refoss.DeviceInfo.Get"
	MethodStatus     = "Refoss.Status.Get"

	maxBody = 1 << 20
)

var allowed = map[string]bool{
	MethodDeviceInfo: true,
	MethodStatus:     true,
}

// ErrAuthRequired means the device answered 401: its local authentication is on.
var ErrAuthRequired = errors.New("refoss: the device asks for a login; turn local authentication off in the Refoss app")

// DeviceInfo is the answer of Refoss.DeviceInfo.Get, which needs no authentication.
type DeviceInfo struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	DevID  string `json:"dev_id"`
	APIVer string `json:"api_ver"`
	FwVer  string `json:"fw_ver"`
	HwVer  string `json:"hw_ver"`
	AuthEn bool   `json:"auth_en"`
}

type Client struct {
	base string
	http *http.Client
}

// New returns a client for the device at host, which must be a plain host name or IP address.
func New(host string, hc *http.Client) (*Client, error) {
	if err := hostcheck.Validate(host); err != nil {
		return nil, fmt.Errorf("refoss host %q: %w", host, err)
	}
	return newWithBase("http://"+host, hc), nil
}

func newWithBase(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{base: base, http: hc}
}

func (c *Client) get(ctx context.Context, method string) ([]byte, error) {
	if !allowed[method] {
		return nil, fmt.Errorf("refoss: method %q is not allowed (this client only reads)", method)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/rpc/"+method, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrAuthRequired
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("refoss: %s answered %s", method, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

func (c *Client) DeviceInfo(ctx context.Context) (DeviceInfo, error) {
	body, err := c.get(ctx, MethodDeviceInfo)
	if err != nil {
		return DeviceInfo{}, err
	}
	var info DeviceInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return DeviceInfo{}, fmt.Errorf("refoss: reading %s: %w", MethodDeviceInfo, err)
	}
	return info, nil
}

// StatusRaw returns the unparsed answer of Refoss.Status.Get.
func (c *Client) StatusRaw(ctx context.Context) ([]byte, error) {
	return c.get(ctx, MethodStatus)
}
