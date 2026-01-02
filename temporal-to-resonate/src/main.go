package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
)

// =============================================================================
// Configuration
// =============================================================================

type Config struct {
	ResonateURL       string
	TemporalHost      string
	TemporalNamespace string
	TemporalTaskQueue string
}

func loadConfig() *Config {
	return &Config{
		ResonateURL:       getEnv("RESONATE_URL", "http://localhost:8001"),
		TemporalHost:      getEnv("TEMPORAL_HOST", "localhost:7233"),
		TemporalNamespace: getEnv("TEMPORAL_NAMESPACE", "default"),
		TemporalTaskQueue: getEnv("TEMPORAL_TASK_QUEUE", "resonate-temporal"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// =============================================================================
// Connector
// =============================================================================

type Connector struct {
	cfg        *Config
	httpClient *http.Client
}

func NewConnector(cfg *Config) *Connector {
	return &Connector{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// DynamicActivity handles any activity by forwarding to Resonate
func (c *Connector) DynamicActivity(ctx context.Context, args converter.EncodedValues) (interface{}, error) {
	// Get activity info
	info := activity.GetInfo(ctx)

	// Log activity details
	log.Println("==================================================")
	log.Println("  Received Activity")
	log.Println("==================================================")
	log.Printf("  Activity Type:    %s", info.ActivityType.Name)
	log.Printf("  Activity ID:      %s", info.ActivityID)
	log.Printf("  Workflow:         %s", info.WorkflowExecution.ID)
	log.Printf("  Run ID:           %s", info.WorkflowExecution.RunID)
	log.Printf("  Task Queue:       %s", info.TaskQueue)
	log.Printf("  Attempt:          %d", info.Attempt)
	log.Println("--------------------------------------------------")
	log.Printf("  Heartbeat Timeout: %v", info.HeartbeatTimeout)
	log.Printf("  Deadline:          %v", info.Deadline)

	// Decode arguments
	var rawArgs []interface{}
	if args.HasValues() {
		if err := args.Get(&rawArgs); err != nil {
			// Try decoding as single value
			var singleArg interface{}
			if err := args.Get(&singleArg); err != nil {
				log.Printf("  Args: (failed to decode: %v)", err)
			} else {
				rawArgs = []interface{}{singleArg}
			}
		}
	}
	log.Printf("  Args: %v", rawArgs)
	log.Println("==================================================")

	// Calculate timeout based on deadline
	timeout := time.Until(info.Deadline)
	if timeout < 0 {
		timeout = 60 * time.Second // Default fallback
	}

	// Create promise ID based on workflow execution and activity
	promiseID := fmt.Sprintf("%s-%s-%s", info.WorkflowExecution.ID, info.WorkflowExecution.RunID, info.ActivityID)

	// Forward to Resonate
	result, err := c.invokeResonate(ctx, info.ActivityType.Name, promiseID, rawArgs, timeout)
	if err != nil {
		log.Printf("Resonate invocation failed: %v", err)
		return nil, err
	}

	log.Printf("Activity %s completed with result: %v", info.ActivityType.Name, result)
	return result, nil
}

// invokeResonate creates a promise and waits for it to complete
func (c *Connector) invokeResonate(ctx context.Context, funcName, promiseID string, args []interface{}, timeout time.Duration) (interface{}, error) {
	info := activity.GetInfo(ctx)

	// Calculate absolute timeout
	timeoutMs := time.Now().Add(timeout).UnixMilli()

	// Create the promise
	log.Printf("Creating Resonate promise: %s (func=%s, timeout=%v)", promiseID, funcName, timeout)

	reqBody := map[string]interface{}{
		"id":      promiseID,
		"timeout": timeoutMs,
		"param": map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString(mustJSON(map[string]interface{}{
				"func": funcName,
				"args": args,
			})),
		},
		"tags": map[string]string{
			"resonate:invoke": "poll://any@default",
		},
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.ResonateURL+"/promises", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", promiseID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create promise: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("failed to create promise: status %d", resp.StatusCode)
	}

	log.Printf("Promise created: %s", promiseID)

	// Poll for promise completion
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-heartbeatTicker.C:
			// Heartbeat to Temporal if needed
			if info.HeartbeatTimeout > 0 {
				activity.RecordHeartbeat(ctx, "waiting for Resonate")
			}

		case <-ticker.C:
			// Check promise status
			result, done, err := c.checkPromise(ctx, promiseID)
			if err != nil {
				log.Printf("Error checking promise: %v", err)
				continue
			}
			if done {
				return result, nil
			}
		}
	}
}

func (c *Connector) checkPromise(ctx context.Context, promiseID string) (interface{}, bool, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.cfg.ResonateURL+"/promises/"+promiseID, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	var promise struct {
		State string `json:"state"`
		Value struct {
			Data string `json:"data"`
		} `json:"value"`
	}
	json.NewDecoder(resp.Body).Decode(&promise)

	switch promise.State {
	case "RESOLVED":
		// Decode the result
		if promise.Value.Data != "" {
			decoded, _ := base64.StdEncoding.DecodeString(promise.Value.Data)
			var result interface{}
			json.Unmarshal(decoded, &result)
			return result, true, nil
		}
		return nil, true, nil

	case "REJECTED":
		if promise.Value.Data != "" {
			decoded, _ := base64.StdEncoding.DecodeString(promise.Value.Data)
			return nil, true, fmt.Errorf("promise rejected: %s", string(decoded))
		}
		return nil, true, fmt.Errorf("promise rejected")

	default:
		// Still pending
		return nil, false, nil
	}
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// =============================================================================
// Main
// =============================================================================

func main() {
	cfg := loadConfig()

	fmt.Println("==================================================")
	fmt.Println("  Resonate Connect: Temporal → Resonate")
	fmt.Println("==================================================")
	fmt.Printf("  Resonate:   %s\n", cfg.ResonateURL)
	fmt.Printf("  Temporal:   %s\n", cfg.TemporalHost)
	fmt.Printf("  Namespace:  %s\n", cfg.TemporalNamespace)
	fmt.Printf("  Task Queue: %s\n", cfg.TemporalTaskQueue)
	fmt.Println("==================================================")
	fmt.Println()

	// Connect to Temporal with retry
	var c client.Client
	var err error
	for i := 0; i < 30; i++ {
		c, err = client.Dial(client.Options{
			HostPort:  cfg.TemporalHost,
			Namespace: cfg.TemporalNamespace,
		})
		if err == nil {
			log.Printf("Connected to Temporal at %s", cfg.TemporalHost)
			break
		}
		log.Printf("Waiting for Temporal... (%d/30)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	// Create connector
	connector := NewConnector(cfg)

	// Create worker
	w := worker.New(c, cfg.TemporalTaskQueue, worker.Options{})

	// Register dynamic activity handler (catch-all for any activity name)
	w.RegisterDynamicActivity(
		connector.DynamicActivity,
		activity.DynamicRegisterOptions{},
	)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		w.Stop()
	}()

	log.Println("Activity bridge started, waiting for activities...")

	// Run worker
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker error: %v", err)
	}
}
