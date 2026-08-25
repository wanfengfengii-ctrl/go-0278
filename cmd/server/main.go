// Command server is the runnable entry point for the rock-bolt pullout
// inspection service. It opens the transactional relational store, seeds the
// default test rig devices and serves the versioned JSON API.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"rockbolt-pullout-zonal-closure/internal/api"
	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dbPath := os.Getenv("DB_PATH")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	svc := service.New(st)
	if err := seedDevices(context.Background(), st); err != nil {
		log.Fatalf("seed devices: %v", err)
	}

	handler := api.NewServer(svc)
	log.Printf("listening on %s (db=%q)", addr, dbPath)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// seedDevices installs the default puller, pump and displacement rig so the
// service is immediately usable for a smoke flow.
func seedDevices(ctx context.Context, st *store.Store) error {
	return st.EnsureDevices(ctx, []domain.Device{
		{Number: "puller-1", Type: domain.DevicePuller, State: "available", CalibVer: "cal-2026-01"},
		{Number: "pump-1", Type: domain.DevicePump, State: "available", CalibVer: "cal-2026-01"},
		{Number: "disp-1", Type: domain.DeviceDisplacement, State: "available", CalibVer: "cal-2026-01"},
	})
}
