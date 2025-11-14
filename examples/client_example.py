"""
Waverless Client Example

This example demonstrates how to use the HTTP API to submit tasks and query status.
"""

import requests
import time
import json

# Waverless service address
BASE_URL = "http://localhost:8080"

def submit_async_task(prompt: str):
    """Submit async task"""
    url = f"{BASE_URL}/v1/run"
    payload = {
        "input": {
            "prompt": prompt
        }
    }

    response = requests.post(url, json=payload)
    response.raise_for_status()

    result = response.json()
    print(f"Task submitted: {result['id']}")
    return result['id']

def submit_sync_task(prompt: str):
    """Submit sync task (wait for result)"""
    url = f"{BASE_URL}/v1/runsync"
    payload = {
        "input": {
            "prompt": prompt
        }
    }

    print(f"Submitting sync task: {prompt}")
    response = requests.post(url, json=payload)
    response.raise_for_status()

    result = response.json()
    print(f"Task completed: {json.dumps(result, indent=2)}")
    return result

def get_task_status(task_id: str):
    """Query task status"""
    url = f"{BASE_URL}/v1/status/{task_id}"

    response = requests.get(url)
    response.raise_for_status()

    return response.json()

def cancel_task(task_id: str):
    """Cancel task"""
    url = f"{BASE_URL}/v1/cancel/{task_id}"

    response = requests.post(url)
    response.raise_for_status()

    return response.json()

def wait_for_task(task_id: str, timeout: int = 60):
    """Wait for task completion"""
    start_time = time.time()

    while time.time() - start_time < timeout:
        status = get_task_status(task_id)
        print(f"Status: {status['status']}")

        if status['status'] in ['COMPLETED', 'FAILED', 'CANCELLED']:
            return status

        time.sleep(2)

    raise TimeoutError(f"Task {task_id} timeout")

def get_workers():
    """Get online Worker list"""
    url = f"{BASE_URL}/v1/workers"

    response = requests.get(url)
    response.raise_for_status()

    workers = response.json()
    print(f"Online workers: {len(workers)}")
    for worker in workers:
        print(f"  - {worker['id']}: {worker['status']}, "
              f"concurrency={worker['concurrency']}, "
              f"current_jobs={worker['current_jobs']}")

    return workers

# Example usage
if __name__ == "__main__":
    print("=== Waverless Client Example ===\n")

    # 1. Get Worker list
    print("1. Getting worker list...")
    try:
        get_workers()
    except Exception as e:
        print(f"Error: {e}")
    print()

    # 2. Submit async task
    print("2. Submitting async task...")
    try:
        task_id = submit_async_task("Hello, Waverless!")

        # Wait for task completion
        print("Waiting for task completion...")
        result = wait_for_task(task_id)
        print(f"Task result: {json.dumps(result, indent=2)}")
    except Exception as e:
        print(f"Error: {e}")
    print()

    # 3. Submit sync task
    print("3. Submitting sync task...")
    try:
        result = submit_sync_task("Sync task example")
    except Exception as e:
        print(f"Error: {e}")
    print()

    # 4. Submit task then cancel
    print("4. Submit and cancel task...")
    try:
        task_id = submit_async_task("This will be cancelled")
        time.sleep(1)

        cancel_result = cancel_task(task_id)
        print(f"Cancel result: {cancel_result}")

        status = get_task_status(task_id)
        print(f"Task status after cancel: {status['status']}")
    except Exception as e:
        print(f"Error: {e}")
    print()

    # 5. Batch submit tasks
    print("5. Batch submit tasks...")
    try:
        task_ids = []
        for i in range(5):
            task_id = submit_async_task(f"Batch task {i}")
            task_ids.append(task_id)

        print(f"Submitted {len(task_ids)} tasks")

        # Wait for all tasks to complete
        print("Waiting for all tasks to complete...")
        for task_id in task_ids:
            try:
                result = wait_for_task(task_id, timeout=30)
                print(f"Task {task_id}: {result['status']}")
            except TimeoutError:
                print(f"Task {task_id}: TIMEOUT")
    except Exception as e:
        print(f"Error: {e}")
