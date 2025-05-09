#!/bin/bash

# This script manually triggers an SSE event with the example data
# Run this script while having the test-sse.html page open in a browser

# Verify we're in the right directory
if [ ! -d "./internal/web/static" ]; then
    echo "Error: Run this script from the root project directory"
    exit 1
fi

echo "Sending test SSE event to meetings..."

# Define the test meeting in the same format as the example
TEST_MEETING='{"id":"96722590573","topic":"AppSec \u0026 Friends","start_time":"0001-01-01T00:00:00Z","end_time":"2025-05-09T10:08:06.15140462+02:00","duration":0,"status":3,"host":{"id":"","name":"","email":"","join_time":"0001-01-01T00:00:00Z","leave_time":"0001-01-01T00:00:00Z"},"participants":[]}'

# Make a POST request to update a meeting, which should trigger an SSE event
curl -X POST http://localhost:8080/api/meetings/96722590573 \
  -H 'Content-Type: application/json' \
  -d "$TEST_MEETING" \
  -v

echo -e "\nEvent sent! Check the SSE test page to see if it was received."