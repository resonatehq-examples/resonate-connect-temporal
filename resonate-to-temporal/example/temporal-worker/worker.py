"""
Example Temporal Worker

Runs workflows that can be triggered via Resonate.

Usage:
    resonate invoke my-task \
        --func GreetingWorkflow \
        --arg "Alice" \
        --target "poll://any@temporal"
"""

import asyncio
import os
from datetime import timedelta

from temporalio import activity, workflow
from temporalio.client import Client
from temporalio.worker import Worker

TEMPORAL_HOST = os.getenv("TEMPORAL_HOST", "localhost:7233")
TASK_QUEUE = "resonate-queue"


# ============================================================================
# Activities
# ============================================================================


@activity.defn
async def greet(name: str) -> str:
    """Simple greeting activity."""
    return f"Hello, {name}!"


@activity.defn
async def process_data(data: list) -> dict:
    """Process some data."""
    result = [x.upper() if isinstance(x, str) else x for x in data]
    return {"input": data, "output": result}


# ============================================================================
# Workflows
# ============================================================================


@workflow.defn
class GreetingWorkflow:
    """Simple greeting workflow."""

    @workflow.run
    async def run(self, name: str) -> str:
        return await workflow.execute_activity(
            greet,
            name,
            start_to_close_timeout=timedelta(seconds=30),
        )


@workflow.defn
class ProcessingWorkflow:
    """Data processing workflow."""

    @workflow.run
    async def run(self, data: list) -> dict:
        return await workflow.execute_activity(
            process_data,
            data,
            start_to_close_timeout=timedelta(seconds=30),
        )


# ============================================================================
# Main
# ============================================================================


async def main():
    print("=" * 50)
    print("  Temporal Worker")
    print("=" * 50)
    print(f"  Host:       {TEMPORAL_HOST}")
    print(f"  Task Queue: {TASK_QUEUE}")
    print("=" * 50)
    print()

    # Retry connection until Temporal is ready
    client = None
    for i in range(30):
        try:
            client = await Client.connect(TEMPORAL_HOST)
            print(f"Connected to Temporal at {TEMPORAL_HOST}")
            break
        except Exception as e:
            print(f"Waiting for Temporal... ({i+1}/30)")
            await asyncio.sleep(2)

    if not client:
        print("Failed to connect to Temporal")
        return

    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[GreetingWorkflow, ProcessingWorkflow],
        activities=[greet, process_data],
    )

    print(f"Worker started, listening on queue: {TASK_QUEUE}")
    print("Available workflows: GreetingWorkflow, ProcessingWorkflow")
    print()

    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
