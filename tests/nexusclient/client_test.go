package nexusclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
	"github.com/uvwt/agentdock/internal/nexusclient"
)

func TestClientEnrollmentHeartbeatAndLongPoll(t *testing.T) {
	var mu sync.Mutex
	requests := make([]string, 0, 3)
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/devices/enroll":
			var request contracts.DeviceEnrollmentRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode enrollment: %v", err)
			}
			if request.EnrollmentToken != "enroll-once" || request.Platform != "darwin" {
				t.Errorf("unexpected enrollment: %#v", request)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(contracts.DeviceEnrollmentResponse{
				DeviceId:                 "device-1",
				DeviceToken:              "device-token",
				TokenExpiresAt:           now.Add(time.Hour).Format(time.RFC3339),
				HeartbeatIntervalSeconds: 30,
				ServerTime:               now.Format(time.RFC3339),
			})
		case "/v1/devices/device-1/heartbeat":
			if got := r.Header.Get("Authorization"); got != "Bearer device-token" {
				t.Errorf("authorization = %q", got)
			}
			var heartbeat contracts.DeviceHeartbeat
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Errorf("decode heartbeat: %v", err)
			}
			if heartbeat.DeviceId != "device-1" {
				t.Errorf("heartbeat device_id = %q", heartbeat.DeviceId)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/devices/device-1/commands/lease":
			_ = json.NewEncoder(w).Encode(testLease(now, "command-1", "lease-1", "idem-1", "health.check", time.Minute))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := nexusclient.New(nexusclient.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	enrolled, err := client.Enroll(ctx, contracts.DeviceEnrollmentRequest{
		EnrollmentToken:  "enroll-once",
		Name:             "DockMini",
		Platform:         "darwin",
		Arch:             "arm64",
		AgentdockVersion: "test",
		PublicKey:        "test-public-key",
		Labels:           json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := nexusclient.DeviceState{DeviceID: enrolled.DeviceId, DeviceToken: enrolled.DeviceToken}
	heartbeat := contracts.DeviceHeartbeat{
		DeviceId:         enrolled.DeviceId,
		SentAt:           now.Format(time.RFC3339),
		AgentdockVersion: "test",
		Metrics:          json.RawMessage(`{}`),
		Capabilities:     []contracts.DeviceCapability{},
	}
	if err := client.Heartbeat(ctx, state, heartbeat); err != nil {
		t.Fatal(err)
	}
	lease, err := client.PollCommand(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if lease == nil || lease.Command.Id != "command-1" {
		t.Fatalf("unexpected lease: %#v", lease)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestClientMapsTokenRevocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(contracts.ErrorResponse{Code: "TOKEN_REVOKED", Message: "revoked", RequestId: "test"})
	}))
	defer server.Close()
	client, err := nexusclient.New(nexusclient.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Heartbeat(context.Background(), nexusclient.DeviceState{DeviceID: "d", DeviceToken: "t"}, contracts.DeviceHeartbeat{})
	if !errors.Is(err, nexusclient.ErrTokenRevoked) {
		t.Fatalf("error = %v, want ErrTokenRevoked", err)
	}
}

func testLease(now time.Time, commandID, leaseID, idempotencyKey, commandType string, leaseDuration time.Duration) contracts.CommandLease {
	return contracts.CommandLease{
		LeaseId:           leaseID,
		LeasedAt:          now.Format(time.RFC3339Nano),
		ExpiresAt:         now.Add(leaseDuration).Format(time.RFC3339Nano),
		RenewAfterSeconds: int64(leaseDuration.Seconds() / 2),
		Command: contracts.DeviceCommand{
			Id:             commandID,
			DeviceId:       "device-1",
			Type:           commandType,
			Status:         "leased",
			Payload:        json.RawMessage(`{}`),
			Risk:           "low",
			IdempotencyKey: idempotencyKey,
			CreatedAt:      now.Format(time.RFC3339Nano),
			ExpiresAt:      now.Add(2 * time.Minute).Format(time.RFC3339Nano),
			Attempt:        1,
			MaxAttempts:    3,
		},
	}
}
