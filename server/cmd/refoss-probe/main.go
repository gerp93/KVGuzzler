// refoss-probe makes the two read-only calls the server needs from a Refoss energy monitor and prints exactly what it
// answers, so the answer can be saved as a test fixture and the server's mapping written from it.
//
//	go run ./cmd/refoss-probe 192.168.1.50
//
// It only reads (Refoss.DeviceInfo.Get and Refoss.Status.Get) and writes nothing to the device.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/refoss"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: refoss-probe <host or IP of the monitor>")
		os.Exit(2)
	}
	c, err := refoss.New(os.Args[1], nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := probe(ctx, c, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// probe prints the device info and the raw status answer.
func probe(ctx context.Context, c *refoss.Client, w io.Writer) error {
	info, err := c.DeviceInfo(ctx)
	if err != nil {
		return explain(err)
	}
	fmt.Fprintf(w, "== %s ==\n", refoss.MethodDeviceInfo)
	pretty, _ := json.MarshalIndent(info, "", "  ")
	fmt.Fprintf(w, "%s\n\n", pretty)
	if info.AuthEn {
		fmt.Fprintln(w, "note: the device says local authentication is ON; the next call will likely be refused (turn it off in the Refoss app).")
		fmt.Fprintln(w)
	}

	body, err := c.StatusRaw(ctx)
	if err != nil {
		return explain(err)
	}
	fmt.Fprintf(w, "== %s (raw answer) ==\n", refoss.MethodStatus)
	var indented bytes.Buffer
	if json.Indent(&indented, body, "", "  ") == nil {
		body = indented.Bytes()
	}
	fmt.Fprintf(w, "%s\n\n", body)
	fmt.Fprintln(w, "Save the status answer above as server/internal/refoss/testdata/status-"+info.Model+".json: the server's mapping is written from it.")
	return nil
}

func explain(err error) error {
	if errors.Is(err, refoss.ErrAuthRequired) {
		return fmt.Errorf("%w (the monitor's local authentication is on)", err)
	}
	return err
}
