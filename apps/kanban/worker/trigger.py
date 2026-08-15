import asyncio
import os
import sys
from datetime import timedelta

from temporalio.client import Client


async def main() -> None:
    if len(sys.argv) < 2:
        print("usage: python trigger.py <ActivityName> <topic...>", file=sys.stderr)
        print('example: python trigger.py GenerateUI "the weather forecast for tomorrow"', file=sys.stderr)
        sys.exit(1)

    activity_name = sys.argv[1]
    topic = " ".join(sys.argv[2:]) or "whatever we were just talking about"

    address = os.environ.get("TEMPORAL_ADDRESS", "localhost:7233")
    task_queue = os.environ.get("TEMPORAL_TASK_QUEUE", "gooey-dynamic-ui-task-queue")
    client = await Client.connect(address)

    # No workflow involved — this is a standalone activity, started directly
    # from the client and run by whatever worker is polling task_queue.
    markup = await client.execute_activity(
        activity_name,
        topic,
        id=f"{activity_name}-{os.urandom(4).hex()}",
        task_queue=task_queue,
        start_to_close_timeout=timedelta(seconds=60),
        result_type=str,
    )
    print(markup)


if __name__ == "__main__":
    asyncio.run(main())
