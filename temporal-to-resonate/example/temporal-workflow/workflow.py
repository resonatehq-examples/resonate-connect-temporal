"""
Example Temporal Workflow that calls activities through the Resonate bridge.

The workflow calls an activity by name, which gets routed to the bridge.
The bridge creates a Resonate promise and waits for a Resonate worker to complete it.
"""

import asyncio
import os
from datetime import timedelta

from temporalio import activity, workflow
from temporalio.client import Client
from temporalio.worker import Worker

TEMPORAL_HOST = os.getenv("TEMPORAL_HOST", "localhost:7233")


@workflow.defn
class GreetingWorkflow:
    """
    A workflow that calls an activity to generate a greeting.

    The activity "greet" will be handled by the Resonate bridge,
    which forwards it to a Resonate worker.
    """

    @workflow.run
    async def run(self, name: str) -> str:
        # Call the "greet" activity - this goes to the bridge
        result = await workflow.execute_activity(
            "greet",  # Activity name - Resonate function name
            name,     # Activity argument
            task_queue="resonate-bridge",  # Bridge task queue
            start_to_close_timeout=timedelta(seconds=60),
            heartbeat_timeout=timedelta(seconds=10),
        )
        return result


@workflow.defn
class DataProcessingWorkflow:
    """
    A workflow that calls multiple activities to process data.
    """

    @workflow.run
    async def run(self, data: list) -> dict:
        # First activity: transform data
        transformed = await workflow.execute_activity(
            "transform",
            data,
            task_queue="resonate-bridge",
            start_to_close_timeout=timedelta(seconds=60),
        )

        # Second activity: aggregate results
        aggregated = await workflow.execute_activity(
            "aggregate",
            transformed,
            task_queue="resonate-bridge",
            start_to_close_timeout=timedelta(seconds=60),
        )

        return {"input": data, "transformed": transformed, "aggregated": aggregated}


async def main():
    print("=" * 50)
    print("  Temporal Workflow Worker")
    print("=" * 50)
    print(f"  Host: {TEMPORAL_HOST}")
    print("=" * 50)
    print()

    # Connect to Temporal with retry
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

    # Run worker for workflows only (no activities - those go to the bridge)
    worker = Worker(
        client,
        task_queue="workflow-queue",
        workflows=[GreetingWorkflow, DataProcessingWorkflow],
    )

    print("Workflow worker started")
    print("Available workflows: GreetingWorkflow, DataProcessingWorkflow")
    print()

    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
