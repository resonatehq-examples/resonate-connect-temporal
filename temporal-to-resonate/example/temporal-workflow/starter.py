"""
Starter script to invoke workflows.

Usage:
    python starter.py greeting Alice
    python starter.py processing '["a", "b", "c"]'
"""

import asyncio
import json
import sys
import os
import uuid

from temporalio.client import Client

TEMPORAL_HOST = os.getenv("TEMPORAL_HOST", "localhost:7233")


async def main():
    if len(sys.argv) < 3:
        print("Usage: python starter.py <workflow> <arg>")
        print("  workflow: greeting | processing")
        print("  arg: string for greeting, JSON array for processing")
        sys.exit(1)

    workflow_type = sys.argv[1]
    arg = sys.argv[2]

    # Parse JSON if processing workflow
    if workflow_type == "processing":
        try:
            arg = json.loads(arg)
        except json.JSONDecodeError:
            print("Error: processing workflow requires a JSON array")
            sys.exit(1)

    # Connect to Temporal
    client = await Client.connect(TEMPORAL_HOST)

    # Generate workflow ID
    workflow_id = f"{workflow_type}-{uuid.uuid4().hex[:8]}"

    print(f"Starting {workflow_type} workflow with ID: {workflow_id}")
    print(f"Argument: {arg}")

    # Start workflow
    if workflow_type == "greeting":
        handle = await client.start_workflow(
            "GreetingWorkflow",
            arg,
            id=workflow_id,
            task_queue="workflow-queue",
        )
    elif workflow_type == "processing":
        handle = await client.start_workflow(
            "DataProcessingWorkflow",
            arg,
            id=workflow_id,
            task_queue="workflow-queue",
        )
    else:
        print(f"Unknown workflow: {workflow_type}")
        sys.exit(1)

    print("Waiting for result...")
    result = await handle.result()
    print(f"Result: {result}")


if __name__ == "__main__":
    asyncio.run(main())
