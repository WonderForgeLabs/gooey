import asyncio
import os

from temporalio.client import Client
from temporalio.worker import Worker

from activities import generate_ui


async def main() -> None:
    address = os.environ.get("TEMPORAL_ADDRESS", "localhost:7233")
    task_queue = os.environ.get("TEMPORAL_TASK_QUEUE", "gooey-dynamic-ui-task-queue")

    client = await Client.connect(address)
    worker = Worker(client, task_queue=task_queue, activities=[generate_ui])

    print(f"worker running on task queue {task_queue!r} against {address}...", flush=True)
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
