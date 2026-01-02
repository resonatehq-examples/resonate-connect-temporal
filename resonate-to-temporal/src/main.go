package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
)

// =============================================================================
// Configuration
// =============================================================================

type Config struct {
	ResonateURL       string
	ResonateGroup     string
	ResonateTTL       int
	TemporalHost      string
	TemporalNamespace string
	TemporalTaskQueue string
}

func loadConfig() *Config {
	return &Config{
		ResonateURL:       getEnv("RESONATE_URL", "http://localhost:8001"),
		ResonateGroup:     getEnv("RESONATE_GROUP", "temporal"),
		ResonateTTL:       300000,
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

type Task struct {
	TaskID    string
	Counter   int
	PromiseID string
	Workflow  string
}

type Connector struct {
	cfg        *Config
	processID  string
	temporal   client.Client
	httpClient *http.Client

	tasks   map[string]*Task
	tasksMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

func NewConnector(cfg *Config) *Connector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connector{
		cfg:        cfg,
		processID:  fmt.Sprintf("connector-%d", os.Getpid()),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tasks:      make(map[string]*Task),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (c *Connector) Run() error {
	// Connect to Temporal with retry
	var err error
	for i := 0; i < 30; i++ {
		c.temporal, err = client.Dial(client.Options{
			HostPort:  c.cfg.TemporalHost,
			Namespace: c.cfg.TemporalNamespace,
		})
		if err == nil {
			log.Printf("Connected to Temporal at %s", c.cfg.TemporalHost)
			break
		}
		log.Printf("Waiting for Temporal... (%d/30)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.temporal.Close()

	// Start background loops
	go c.heartbeatLoop()
	go c.monitorLoop()

	// Main SSE loop
	c.sseLoop()
	return nil
}

func (c *Connector) Shutdown() {
	c.cancel()
}

// =============================================================================
// SSE Loop - Listen for tasks from Resonate
// =============================================================================

func (c *Connector) sseLoop() {
	sseURL := fmt.Sprintf("%s/poll/%s/%s", c.cfg.ResonateURL, c.cfg.ResonateGroup, c.processID)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		log.Printf("Connecting to SSE: %s", sseURL)

		req, _ := http.NewRequestWithContext(c.ctx, "GET", sseURL, nil)
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("SSE error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		log.Println("SSE connected, waiting for tasks...")

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				c.handleTask(line[6:])
			}
		}
		resp.Body.Close()
		time.Sleep(5 * time.Second)
	}
}

func (c *Connector) handleTask(data string) {
	var msg struct {
		Task struct {
			ID            string `json:"id"`
			Counter       int    `json:"counter"`
			RootPromiseID string `json:"rootPromiseId"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return
	}

	taskID := msg.Task.ID
	counter := msg.Task.Counter
	promiseID := msg.Task.RootPromiseID

	log.Printf("Received task: %s", taskID)

	// Claim task
	claimResp, err := c.claim(taskID, counter)
	if err != nil {
		log.Printf("Claim failed: %v", err)
		return
	}

	// Decode workflow and args
	workflow, args := c.decodeParams(claimResp)
	if workflow == "" {
		c.completePromise(promiseID, map[string]string{"error": "No workflow type"}, true)
		c.completeTask(taskID, counter)
		return
	}

	log.Printf("Task %s: workflow=%s", taskID, workflow)

	// Start Temporal workflow
	workflowID := fmt.Sprintf("resonate-%s", promiseID)
	_, err = c.temporal.ExecuteWorkflow(c.ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: c.cfg.TemporalTaskQueue,
	}, workflow, args...)

	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already") {
		c.completePromise(promiseID, map[string]string{"error": err.Error()}, true)
		c.completeTask(taskID, counter)
		return
	}

	log.Printf("Started workflow %s", workflowID)

	// Track task
	c.tasksMu.Lock()
	c.tasks[taskID] = &Task{taskID, counter, promiseID, workflowID}
	c.tasksMu.Unlock()
}

// =============================================================================
// Monitor Loop - Check workflow status and complete promises
// =============================================================================

func (c *Connector) monitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkWorkflows()
		}
	}
}

func (c *Connector) checkWorkflows() {
	c.tasksMu.RLock()
	tasks := make([]*Task, 0, len(c.tasks))
	for _, t := range c.tasks {
		tasks = append(tasks, t)
	}
	c.tasksMu.RUnlock()

	for _, task := range tasks {
		desc, err := c.temporal.DescribeWorkflowExecution(c.ctx, task.Workflow, "")
		if err != nil {
			continue
		}

		status := desc.WorkflowExecutionInfo.Status.String()

		if status == "Completed" {
			var result interface{}
			run := c.temporal.GetWorkflow(c.ctx, task.Workflow, "")
			run.Get(c.ctx, &result)

			c.completePromise(task.PromiseID, map[string]interface{}{
				"status":      "completed",
				"workflow_id": task.Workflow,
				"result":      result,
			}, false)
			c.completeTask(task.TaskID, task.Counter)
			log.Printf("Task %s: workflow completed", task.TaskID)

			c.tasksMu.Lock()
			delete(c.tasks, task.TaskID)
			c.tasksMu.Unlock()

		} else if status == "Failed" || status == "Canceled" || status == "Terminated" || status == "TimedOut" {
			c.completePromise(task.PromiseID, map[string]interface{}{
				"error":       fmt.Sprintf("Workflow %s", strings.ToLower(status)),
				"workflow_id": task.Workflow,
			}, true)
			c.completeTask(task.TaskID, task.Counter)
			log.Printf("Task %s: workflow %s", task.TaskID, strings.ToLower(status))

			c.tasksMu.Lock()
			delete(c.tasks, task.TaskID)
			c.tasksMu.Unlock()
		}
	}
}

// =============================================================================
// Heartbeat Loop - Keep tasks alive
// =============================================================================

func (c *Connector) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.tasksMu.RLock()
			count := len(c.tasks)
			c.tasksMu.RUnlock()

			if count > 0 {
				body, _ := json.Marshal(map[string]string{"processId": c.processID})
				c.httpClient.Post(c.cfg.ResonateURL+"/tasks/heartbeat", "application/json", bytes.NewReader(body))
			}
		}
	}
}

// =============================================================================
// Resonate API Helpers
// =============================================================================

func (c *Connector) claim(taskID string, counter int) (map[string]interface{}, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"id":        taskID,
		"counter":   counter,
		"processId": c.processID,
		"ttl":       c.cfg.ResonateTTL,
	})

	resp, err := c.httpClient.Post(c.cfg.ResonateURL+"/tasks/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("claim failed: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func (c *Connector) decodeParams(claimResp map[string]interface{}) (string, []interface{}) {
	promises, _ := claimResp["promises"].(map[string]interface{})
	root, _ := promises["root"].(map[string]interface{})
	data, _ := root["data"].(map[string]interface{})
	param, _ := data["param"].(map[string]interface{})
	paramData, _ := param["data"].(string)

	if paramData == "" {
		return "", nil
	}

	decoded, _ := base64.StdEncoding.DecodeString(paramData)
	var params struct {
		Func string        `json:"func"`
		Args []interface{} `json:"args"`
	}
	json.Unmarshal(decoded, &params)
	return params.Func, params.Args
}

func (c *Connector) completePromise(promiseID string, result interface{}, isError bool) {
	resultJSON, _ := json.Marshal(result)
	encoded := base64.StdEncoding.EncodeToString(resultJSON)

	state := "RESOLVED"
	if isError {
		state = "REJECTED"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"state": state,
		"value": map[string]string{"data": encoded},
	})

	req, _ := http.NewRequest("PATCH", c.cfg.ResonateURL+"/promises/"+promiseID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := c.httpClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
}

func (c *Connector) completeTask(taskID string, counter int) {
	body, _ := json.Marshal(map[string]interface{}{"id": taskID, "counter": counter})
	resp, _ := c.httpClient.Post(c.cfg.ResonateURL+"/tasks/complete", "application/json", bytes.NewReader(body))
	if resp != nil {
		resp.Body.Close()
	}
}

// =============================================================================
// Main
// =============================================================================

func main() {
	cfg := loadConfig()

	fmt.Println("==================================================")
	fmt.Println("  Resonate Connect: Resonate → Temporal")
	fmt.Println("==================================================")
	fmt.Printf("  Resonate:   %s\n", cfg.ResonateURL)
	fmt.Printf("  Group:      %s\n", cfg.ResonateGroup)
	fmt.Printf("  Temporal:   %s\n", cfg.TemporalHost)
	fmt.Printf("  Namespace:  %s\n", cfg.TemporalNamespace)
	fmt.Printf("  Task Queue: %s\n", cfg.TemporalTaskQueue)
	fmt.Println("==================================================")
	fmt.Println()

	connector := NewConnector(cfg)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		connector.Shutdown()
	}()

	if err := connector.Run(); err != nil {
		log.Fatalf("Connector error: %v", err)
	}
}
