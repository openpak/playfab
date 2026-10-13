// Command playfabd implements OpenPak's supported PlayFab title APIs. nx-baas
// forwards console HTTP here after terminating TLS; Nintendo verification
// remains on nx-baas's authenticated internal listener.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := flag.String("addr", env("PLAYFAB_HTTP_ADDR", ":22650"), "PlayFab HTTP address")
	health := flag.String("health-addr", env("PLAYFAB_HEALTH_ADDR", ":22651"), "internal health address")
	dir := flag.String("state", env("PLAYFAB_STATE_DIR", "/app/state"), "persistent state directory")
	qosAddr := flag.String("qos-addr", os.Getenv("PLAYFAB_QOS_ADDR"), "optional Party UDP beacon address (port 3075)")
	qosHost := flag.String("qos-host", os.Getenv("PLAYFAB_QOS_HOST"), "reachable IPv4 address of the Party beacon")
	flag.Parse()
	key := env("NX_INTERNAL_KEY", os.Getenv("OPENPAK_INTERNAL_KEY"))
	if key == "" {
		log.Fatal("[playfab] NX_INTERNAL_KEY is required")
	}
	state, err := openStore(*dir)
	if err != nil {
		log.Fatalf("[playfab] state: %v", err)
	}
	defer state.close()
	s := &server{state: state, ids: newIdentityClient(env("NX_INTERNAL_URL", "http://127.0.0.1:20070"), key), now: time.Now, titles: defaultTitles()}
	if *qosAddr != "" || *qosHost != "" {
		if *qosAddr == "" || *qosHost == "" {
			log.Fatal("[playfab] both PLAYFAB_QOS_ADDR and PLAYFAB_QOS_HOST are required")
		}
		_, port, err := net.SplitHostPort(*qosAddr)
		if err != nil || port != "3075" {
			log.Fatal("[playfab] Party QoS must listen on UDP port 3075")
		}
		s.qos, err = openPartyQos(*qosAddr, *qosHost)
		if err != nil {
			log.Fatalf("[playfab] QoS: %v", err)
		}
		defer s.qos.conn.Close()
	}
	api := &http.Server{Addr: *addr, Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		data := map[string]any{"status": "ok"}
		if s.qos != nil {
			data["qos_probes_served"] = s.qos.served.Load()
		}
		writeJSON(w, 200, data)
	})
	h := &http.Server{Addr: *health, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	apiListener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	healthListener, err := net.Listen("tcp", *health)
	if err != nil {
		apiListener.Close()
		log.Fatal(err)
	}
	errCh := make(chan error, 3)
	go func() { errCh <- api.Serve(apiListener) }()
	go func() { errCh <- h.Serve(healthListener) }()
	if s.qos != nil {
		go func() { errCh <- s.qos.serve() }()
		log.Printf("[playfab] Party QoS beacon bound on %s (%s)", *qosAddr, partyQosRegion)
	}
	log.Printf("[playfab] API on %s for %d title(s); health on %s", *addr, len(s.titles), *health)
	for _, cfg := range s.titles {
		log.Printf("[playfab] title=%s app=%s host=%s", cfg.Title, cfg.Application, cfg.Host)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[playfab] listener stopped: %v", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	_ = h.Shutdown(shutdown)
}
