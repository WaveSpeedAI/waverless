#!/usr/bin/env python3
"""
Test Worker - for demonstrating Waverless complete workflow
"""
import os
import time
from runpod.serverless import start

# Configure Waverless address
os.environ["RUNPOD_WEBHOOK_GET_JOB"] = "http://localhost:8080/runpod/job-take/$ID"
os.environ["RUNPOD_WEBHOOK_PING"] = "http://localhost:8080/runpod/ping/$ID"
os.environ["RUNPOD_POD_ID"] = "test-worker-001"
os.environ["RUNPOD_PING_INTERVAL"] = "10000"

def handler(job):
    """
    Simple task handler
    """
    print(f"\n{'='*50}")
    print(f"Processing job: {job['id']}")
    print(f"Input: {job.get('input', {})}")
    print(f"{'='*50}\n")

    job_input = job.get("input", {})
    prompt = job_input.get("prompt", "")

    # Simulate processing time
    print("Processing...")
    time.sleep(3)

    result = {
        "result": f"Processed: {prompt}",
        "worker_id": os.environ["RUNPOD_POD_ID"],
        "timestamp": time.time()
    }

    print(f"\n{'='*50}")
    print(f"Job {job['id']} completed")
    print(f"Result: {result}")
    print(f"{'='*50}\n")

    return result

if __name__ == "__main__":
    print("\n" + "="*70)
    print("Starting Waverless Test Worker")
    print("="*70)
    print(f"Worker ID: {os.environ['RUNPOD_POD_ID']}")
    print(f"Waverless URL: {os.environ['RUNPOD_WEBHOOK_GET_JOB'].replace('$ID', os.environ['RUNPOD_POD_ID'])}")
    print("="*70 + "\n")

    start({"handler": handler})
