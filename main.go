package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pboyd-oss/platform-attest-coordinator/coordinator"
	"github.com/pboyd-oss/platform-attest-coordinator/webhook"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	jenkinsURL := envOrDefault("JENKINS_URL", "http://jenkins-operator-http-jenkins.jenkins.svc.cluster.local:8080")
	jenkinsUser := mustEnv("JENKINS_USER")
	jenkinsToken := mustEnv("JENKINS_TOKEN")

	auditURL := envOrDefault("AUDIT_SERVICE_URL", "http://platform-audit-service.platform.svc.cluster.local:8080")
	cedarURL := envOrDefault("CEDAR_SERVICE_URL", "http://platform-cedar-sidecar.platform.svc.cluster.local:8080")
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")

	jenkins := coordinator.NewJenkinsClient(jenkinsURL, jenkinsUser, jenkinsToken)
	cedar := coordinator.NewCedarClient(cedarURL)
	audit := coordinator.NewAuditClient(auditURL)

	coord := coordinator.New(jenkins, cedar, audit, logger)
	handler := webhook.NewHandler(coord, webhookSecret, logger)

	logger.Printf("attest-coordinator listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, handler); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}
