"""
Waverless Worker Example

This example demonstrates how to use runpod-python client to connect to Waverless service.
Fully compatible with existing runpod code, only environment variables need to be modified.
"""

import os
import time
from runpod.serverless import start

# Configure Waverless service address
# $ID will be replaced with actual worker ID (handled automatically by runpod client)
os.environ["RUNPOD_WEBHOOK_GET_JOB"] = "http://localhost:8080/runpod/job-take/$ID"
os.environ["RUNPOD_WEBHOOK_PING"] = "http://localhost:8080/runpod/ping/$ID"
os.environ["RUNPOD_POD_ID"] = "worker-001"  # Worker ID
os.environ["RUNPOD_PING_INTERVAL"] = "10000"  # Heartbeat interval 10 seconds

# Task handler function
def handler(job):
    """
    Main function to handle tasks

    Args:
        job: Task object containing id and input fields

    Returns:
        dict: Result dictionary
    """
    print(f"Processing job: {job['id']}")

    # Get input parameters
    job_input = job.get("input", {})
    prompt = job_input.get("prompt", "")

    # Simulate processing
    print(f"Processing prompt: {prompt}")
    time.sleep(2)  # Simulate time-consuming operation

    # Return result
    result = {
        "result": f"Processed: {prompt}",
        "timestamp": time.time()
    }

    print(f"Job {job['id']} completed")
    return result

# Handler function (with error handling)
def handler_with_error_handling(job):
    """Handler with error handling"""
    try:
        job_input = job.get("input", {})

        # Simulate possible failure
        if job_input.get("should_fail"):
            raise Exception("Simulated failure")

        time.sleep(1)
        return {
            "result": "success",
            "input": job_input
        }
    except Exception as e:
        # Return error
        return {
            "error": str(e)
        }

# Streaming output example (generator)
def streaming_handler(job):
    """
    Streaming output handler
    Using generator to implement streaming result return
    """
    job_input = job.get("input", {})

    # Simulate streaming output
    for i in range(5):
        time.sleep(0.5)
        yield {
            "chunk": i,
            "message": f"Processing step {i}"
        }

    # Finally return complete result
    yield {
        "chunk": "final",
        "result": "completed"
    }

# Custom concurrency
def concurrency_modifier(current_concurrency):
    """
    Customize concurrency

    Args:
        current_concurrency: Current concurrency

    Returns:
        int: Desired concurrency
    """
    return 2  # Set concurrency to 2

# Start Worker
if __name__ == "__main__":
    print("Starting Waverless Worker...")
    print(f"Worker ID: {os.environ['RUNPOD_POD_ID']}")
    print(f"Server URL: {os.environ['RUNPOD_WEBHOOK_GET_JOB']}")

    # Basic start
    # start({"handler": handler})

    # Start with concurrency control
    start({
        "handler": handler,
        "concurrency_modifier": concurrency_modifier
    })

    # Use streaming output
    # start({"handler": streaming_handler})
