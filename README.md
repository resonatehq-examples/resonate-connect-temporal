# Resonate Connect: Temporal

> **Experimental**: This project is experimental and under active development.

Bidirectional integration between Resonate and Temporal.

## Projects

| Project | Direction | Description |
|---------|-----------|-------------|
| [resonate-to-temporal](./resonate-to-temporal) | Resonate → Temporal | Call Temporal workflows from Resonate functions |
| [temporal-to-resonate](./temporal-to-resonate) | Temporal → Resonate | Call Resonate functions from Temporal workflows |

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │                                     │
    ┌───────────┐   │   ┌─────────────────────────────┐   │   ┌───────────┐
    │           │───┼──>│     resonate-to-temporal    │───┼──>│           │
    │           │   │   │         (Connector)         │   │   │           │
    │ Resonate  │   │   └─────────────────────────────┘   │   │ Temporal  │
    │           │   │                                     │   │           │
    │           │<──┼───┌─────────────────────────────┐<──┼───│           │
    │           │   │   │     temporal-to-resonate    │   │   │           │
    └───────────┘   │   │         (Connector)         │   │   └───────────┘
                    │   └─────────────────────────────┘   │
                    │                                     │
                    │        resonate-connect-temporal    │
                    └─────────────────────────────────────┘
```

## Quick Start

Each project has its own example with Docker Compose:

```bash
# Resonate calling Temporal
cd resonate-to-temporal/example
docker compose up

# Temporal calling Resonate
cd temporal-to-resonate/example
docker compose up
```

## License

MIT
