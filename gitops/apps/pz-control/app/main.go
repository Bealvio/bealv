package main

import (
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	password := mustEnv("PZCTL_PASSWORD")
	signingHex := mustEnv("PZCTL_SIGNING_KEY")
	signingKey, err := hex.DecodeString(signingHex)
	if err != nil || len(signingKey) < 16 {
		log.Fatalf("PZCTL_SIGNING_KEY must be hex-encoded and >= 16 bytes: %v", err)
	}

	namespace := envOr("PZCTL_NAMESPACE", "project-zomboid")
	deployment := envOr("PZCTL_DEPLOYMENT", "project-zomboid")
	podLabel := envOr("PZCTL_POD_LABEL", "app=project-zomboid")
	addr := envOr("PZCTL_ADDR", ":8080")

	kube, err := NewKubeClient(namespace, deployment, podLabel)
	if err != nil {
		log.Fatalf("kube client: %v", err)
	}

	srv := &Server{
		Kube:         kube,
		Auth:         NewAuth(password, signingKey, 24*time.Hour),
		Cooldown:     NewCooldown(2 * time.Minute),
		SecureCookie: true,
	}

	log.Printf("pz-control listening on %s (ns=%s deploy=%s)", addr, namespace, deployment)
	log.Fatal(http.ListenAndServe(addr, srv.Router()))
}
