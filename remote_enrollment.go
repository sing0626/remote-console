package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	connect "remoteconsole.local/connect/client"
	"remoteconsole.local/connect/envelope"
	"remoteconsole.local/connect/trust"
	"strings"
	"time"
	"webscreen/webservice"
)

const enrollmentWork = "/data/local/tmp/remote-console"

func printEnrollmentIdentity() error {
	key, err := connect.Identity(filepath.Join(enrollmentWork, "enrollment.key"))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(envelope.PublicPEM(&key.PublicKey))
	return err
}
func enrollmentStatus(state string) {
	data, _ := json.Marshal(map[string]any{"state": state, "updated_at": time.Now().Unix()})
	_ = os.WriteFile(filepath.Join(enrollmentWork, "enrollment-status.json"), data, 0600)
}
func runEnrollment(ctx context.Context, wm *webservice.WebMaster) {
	data, err := os.ReadFile(filepath.Join(enrollmentWork, "enrollment-url"))
	if err != nil {
		return
	}
	origin := strings.TrimSpace(string(data))
	if _, err = connect.ServerURL(origin); err != nil {
		log.Print("[remote] invalid enrollment server origin")
		return
	}
	key, err := connect.Identity(filepath.Join(enrollmentWork, "enrollment.key"))
	if err != nil {
		log.Print("[remote] cannot load device enrollment identity")
		return
	}
	dir := filepath.Join(enrollmentWork, "remote")
	var transport http.RoundTripper
	if address, readErr := os.ReadFile(filepath.Join(enrollmentWork, "enrollment-address")); readErr == nil {
		transport, err = connect.TransportForServer(origin, strings.TrimSpace(string(address)))
		if err != nil {
			log.Print("[remote] invalid enrollment server address")
			return
		}
	}
	for !connect.Cached(dir, origin) {
		enrollmentStatus("waiting_for_server")
		bundle, fetchErr := connect.FetchWithTransport(ctx, origin, key, transport)
		if fetchErr == nil {
			if fetchErr = connect.Save(dir, origin, bundle); fetchErr == nil {
				break
			}
		}
		if ctx.Err() != nil {
			return
		}
		log.Print("[remote] automatic enrollment pending; will retry")
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
	ice, err := webservice.ReadICEConfiguration(filepath.Join(dir, "ice.private.json"))
	if err != nil {
		enrollmentStatus("invalid_ice_profile")
		return
	}
	wm.SetICEConfiguration(ice)
	if profile, readErr := os.ReadFile(filepath.Join(dir, "phone-frpc.toml")); readErr == nil {
		fingerprint := envelope.Fingerprint(&key.PublicKey)
		if authKey, deriveErr := trust.DeriveKey(profile, fingerprint); deriveErr == nil {
			if configureErr := wm.ConfigureGatewayTrust(origin, fingerprint, authKey, transport); configureErr != nil {
				log.Print("[remote] controller approval is unavailable")
			}
		}
	}
	// The existing server startup path also accepts this private ICE file.
	if data, err = os.ReadFile(filepath.Join(dir, "ice.private.json")); err == nil {
		_ = os.WriteFile(filepath.Join(enrollmentWork, "ice.private.json"), data, 0600)
	}
	binary := filepath.Join(enrollmentWork, "frpc")
	for ctx.Err() == nil {
		enrollmentStatus("connecting_tunnel")
		output, err := os.OpenFile(filepath.Join(enrollmentWork, "frpc.private.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		command := exec.CommandContext(ctx, binary, "-c", filepath.Join(dir, "phone-frpc.toml"))
		command.Stdout = output
		command.Stderr = output
		if err = command.Start(); err == nil {
			enrollmentStatus("tunnel_process_running")
			_ = command.Wait()
		}
		output.Close()
		if ctx.Err() != nil {
			return
		}
		enrollmentStatus("reconnecting_tunnel")
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}
