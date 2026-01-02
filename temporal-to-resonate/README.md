# Resonate Connect: Temporal → Resonate

> **Experimental**: This project is experimental and under active development.

Call Resonate functions from Temporal workflows (as activities).

## How It Works

```
┌───────────┐      ┌───────────┐      ┌───────────┐      ┌───────────┐
│ Temporal  │      │           │      │           │      │ Resonate  │
│ Workflow  │─────>│ Connector │─────>│ Resonate  │─────>│ Function  │
│           │      │           │      │ Server    │      │           │
│  activity │      │  create   │      │           │      │  execute  │
│           │<─────│  promise  │<─────│           │<─────│  result   │
└───────────┘      └───────────┘      └───────────┘      └───────────┘
```

1. Temporal workflow executes an activity on the connector's task queue
2. Connector receives the activity and creates a Resonate durable promise
3. Resonate worker picks up the promise and executes the function
4. Result flows back through the connector to complete the Temporal activity

## Quick Start

```bash
cd example
docker compose up -d

# Wait for services to be ready (~30s)
sleep 30

# Run a workflow
docker compose exec temporal-workflow python -c "
import asyncio
from temporalio.client import Client

async def main():
    client = await Client.connect('temporal:7233')
    handle = await client.start_workflow(
        'GreetingWorkflow', 'World',
        id='test', task_queue='workflow-queue'
    )
    result = await handle.result()
    print(f'Result: {result}')

asyncio.run(main())
"
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `RESONATE_URL` | `http://localhost:8001` | Resonate server URL |
| `TEMPORAL_HOST` | `localhost:7233` | Temporal server address |
| `TEMPORAL_NAMESPACE` | `default` | Temporal namespace |
| `TEMPORAL_TASK_QUEUE` | `resonate-temporal` | Task queue for activities |

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
    ├── temporal-workflow/ # Example Temporal workflows
    └── resonate-worker/   # Example Resonate functions
```

## License

MIT
