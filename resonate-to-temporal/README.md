# Resonate Connect: Resonate → Temporal

> **Experimental**: This project is experimental and under active development.

Call Temporal workflows from Resonate functions.

## How It Works

```
┌───────────┐      ┌───────────┐      ┌───────────┐      ┌───────────┐
│ Resonate  │      │           │      │           │      │ Temporal  │
│ Function  │─────>│ Resonate  │─────>│ Connector │─────>│ Workflow  │
│           │      │ Server    │      │           │      │           │
│  invoke   │      │           │      │  claim    │      │  start    │
│           │<─────│           │<─────│  complete │<─────│  result   │
└───────────┘      └───────────┘      └───────────┘      └───────────┘
```

1. Resonate function invokes a promise targeting the Temporal group
2. Connector claims the task via SSE and starts a Temporal workflow
3. Connector monitors the workflow until completion
4. Result flows back through the connector to resolve the Resonate promise

## Quick Start

```bash
cd example
docker compose up -d

# Wait for services to be ready (~30s)
sleep 30

# Invoke a workflow via Resonate
curl -X POST http://localhost:8001/promises \
  -H "Content-Type: application/json" \
  -d '{
    "id": "greeting-1",
    "timeout": 30000,
    "param": {"data": "eyJmdW5jIjoiR3JlZXRpbmdXb3JrZmxvdyIsImFyZ3MiOlsiQWxpY2UiXX0="},
    "tags": {"resonate:invoke": "poll://any@temporal"}
  }'

# Check result
curl http://localhost:8001/promises/greeting-1
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `RESONATE_URL` | `http://localhost:8001` | Resonate server URL |
| `RESONATE_GROUP` | `temporal` | SSE poll group name |
| `TEMPORAL_HOST` | `localhost:7233` | Temporal server address |
| `TEMPORAL_NAMESPACE` | `default` | Temporal namespace |
| `TEMPORAL_TASK_QUEUE` | `resonate-temporal` | Task queue for workflows |

## Building

```bash
go build -o main ./src
```

## Project Structure

```
.
├── src/main.go          # Connector implementation
├── Dockerfile
├── go.mod
├── go.sum
└── example/
    ├── docker-compose.yml
    ├── index.ts         # Example Resonate client
    └── temporal-worker/ # Example Temporal workflows
```

## License

MIT
