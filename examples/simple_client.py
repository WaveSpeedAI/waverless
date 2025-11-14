#!/usr/bin/env python3
"""
Waverless Client Example

Demonstrates how to use Waverless API to submit tasks and query status
"""

import requests
import time
import json

# Waverless API address
BASE_URL = "http://localhost:8080"

# Endpoint name (choose based on deployed Worker)
ENDPOINT = "flux-trainer"


def submit_async_task():
    """Submit async task"""
    print("=== Submit Async Task ===")

    url = f"{BASE_URL}/v1/{ENDPOINT}/run"
    payload = {
        "input": {
            "data": "https://example.com/training-data.zip",
            "steps": 1000,
            "lora_rank": 32,
            "trigger_word": "TOK"
        }
    }

    response = requests.post(url, json=payload)
    result = response.json()

    print(f"Task submitted:")
    print(f"  Task ID: {result['id']}")
    print(f"  Status: {result['status']}")
    print()

    return result['id']


def submit_sync_task():
    """Submit sync task (wait for result)"""
    print("=== Submit Sync Task ===")

    url = f"{BASE_URL}/v1/{ENDPOINT}/runsync"
    payload = {
        "input": {
            "prompt": "a beautiful landscape",
            "steps": 20
        }
    }

    print("Submitting task and waiting for result...")
    response = requests.post(url, json=payload)
    result = response.json()

    print(f"Task completed:")
    print(f"  Task ID: {result['id']}")
    print(f"  Status: {result['status']}")
    if 'output' in result:
        print(f"  Output: {json.dumps(result['output'], indent=2)}")
    print()

    return result


def get_task_status(task_id):
    """Query task status"""
    print(f"=== Query Task Status: {task_id} ===")

    url = f"{BASE_URL}/v1/status/{task_id}"
    response = requests.get(url)
    result = response.json()

    print(f"Task status:")
    print(f"  ID: {result['id']}")
    print(f"  Status: {result['status']}")
    print(f"  Created: {result.get('created_at', 'N/A')}")
    if result.get('started_at'):
        print(f"  Started: {result['started_at']}")
    if result.get('completed_at'):
        print(f"  Completed: {result['completed_at']}")
    if result.get('error'):
        print(f"  Error: {result['error']}")
    if result.get('output'):
        print(f"  Output: {json.dumps(result['output'], indent=2)}")
    print()

    return result


def cancel_task(task_id):
    """Cancel task"""
    print(f"=== Cancel Task: {task_id} ===")

    url = f"{BASE_URL}/v1/cancel/{task_id}"
    response = requests.post(url)
    result = response.json()

    print(f"Result: {result.get('message', 'Unknown')}")
    print()


def get_workers():
    """Get Worker list"""
    print("=== Worker List ===")

    url = f"{BASE_URL}/v1/workers"
    response = requests.get(url)
    workers = response.json()

    print(f"Online Workers: {len(workers)}")
    for worker in workers:
        print(f"\n  Worker ID: {worker['id']}")
        print(f"    Endpoint: {worker['endpoint']}")
        print(f"    Status: {worker['status']}")
        print(f"    Concurrency: {worker['current_jobs']}/{worker['concurrency']}")
        print(f"    Jobs in Progress: {worker.get('jobs_in_progress', [])}")
        print(f"    Last Heartbeat: {worker['last_heartbeat']}")
    print()

    return workers


def get_endpoint_stats(endpoint):
    """Get Endpoint statistics"""
    print(f"=== Endpoint Statistics: {endpoint} ===")

    url = f"{BASE_URL}/v1/{endpoint}/stats"
    response = requests.get(url)
    stats = response.json()

    print(f"Statistics:")
    print(f"  Pending: {stats.get('pending_tasks', 0)}")
    print(f"  In Progress: {stats.get('in_progress_tasks', 0)}")
    print(f"  Completed: {stats.get('completed_tasks', 0)}")
    print(f"  Failed: {stats.get('failed_tasks', 0)}")
    print(f"  Workers: {stats.get('online_workers', 0)}")
    print()

    return stats


def wait_for_task_completion(task_id, timeout=300, interval=5):
    """Wait for task completion"""
    print(f"Waiting for task completion: {task_id}")

    start_time = time.time()
    while time.time() - start_time < timeout:
        result = get_task_status(task_id)

        if result['status'] in ['COMPLETED', 'FAILED', 'CANCELLED']:
            print(f"Task completed: {result['status']}")
            return result

        print(f"Task status: {result['status']}, waiting {interval} seconds...")
        time.sleep(interval)

    print(f"Wait timeout ({timeout} seconds)")
    return None


def main():
    """Main function - demonstrates complete workflow"""
    print("\n" + "=" * 60)
    print("Waverless Client Example")
    print("=" * 60 + "\n")

    try:
        # 1. Check service health
        print("Checking service health...")
        response = requests.get(f"{BASE_URL}/health")
        if response.json().get('status') == 'ok':
            print("✓ Service healthy\n")
        else:
            print("✗ Service unhealthy\n")
            return

        # 2. View Workers
        workers = get_workers()
        if not workers:
            print("Warning: No online Workers available")
            print("Tasks will remain in PENDING status until Workers come online\n")

        # 3. View Endpoint statistics
        get_endpoint_stats(ENDPOINT)

        # 4. Submit async task
        task_id = submit_async_task()

        # 5. Poll for status (example)
        print(f"Query task status (after 5 seconds)...")
        time.sleep(5)
        get_task_status(task_id)

        # 6. Optional: Wait for task completion
        # result = wait_for_task_completion(task_id, timeout=300, interval=5)

        # 7. Optional: Cancel task
        # cancel_task(task_id)

        # 8. Submit sync task (will wait for result)
        # submit_sync_task()

        print("=" * 60)
        print("Example completed!")
        print("=" * 60 + "\n")

    except requests.exceptions.ConnectionError:
        print("\nError: Unable to connect to Waverless")
        print("Please ensure the service is running:")
        print("  kubectl port-forward -n wavespeed svc/waverless-svc 8080:80\n")
    except Exception as e:
        print(f"\nError: {e}\n")


if __name__ == "__main__":
    main()
