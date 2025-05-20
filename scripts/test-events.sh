#!/bin/bash

# Test script for Zrooms
# This script tests the Zrooms application by sending Zoom webhook events
# and verifying expected behavior.

# Configuration
HOST="http://localhost:8080"
WEBHOOK_ENDPOINT="/webhook"
API_ENDPOINT="/api/meetings"
HEADER="Content-Type: application/json"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Counter for passed and failed tests
PASSED=0
FAILED=0

# Display intro message
echo -e "${BLUE}Zrooms Test Suite${NC}"
echo -e "This script will test your Zrooms application."
echo -e "Make sure your application is running at ${HOST}\n"

# Utility function to send webhook events
send_event() {
    local event_type=$1
    local payload=$2
    
    echo -e "${YELLOW}Sending ${event_type} event...${NC}"
    
    local response=$(curl -s -X POST -H "${HEADER}" -d "${payload}" "${HOST}${WEBHOOK_ENDPOINT}")
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Event sent successfully${NC}"
        return 0
    else
        echo -e "${RED}✗ Failed to send event${NC}"
        return 1
    fi
}

# Utility function to verify API response
verify_api() {
    local endpoint=$1
    local expected_count=$2
    local description=$3
    
    echo -e "${YELLOW}Verifying $description...${NC}"
    
    local response=$(curl -s -X GET "${HOST}${endpoint}")
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}✗ Failed to call API${NC}"
        ((FAILED++))
        return 1
    fi
    
    # Count the number of meetings in the response
    local count=$(echo "$response" | grep -o '"id"' | wc -l)
    
    if [ "$count" -eq "$expected_count" ]; then
        echo -e "${GREEN}✓ Test passed: Found $count meetings as expected${NC}"
        ((PASSED++))
        return 0
    else
        echo -e "${RED}✗ Test failed: Expected $expected_count meetings, but found $count${NC}"
        ((FAILED++))
        return 1
    fi
}

# Test 1: Create a meeting
echo -e "\n${BLUE}Test 1: Create a meeting${NC}"
send_event "meeting.created" '{
    "event": "meeting.created",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "test-uuid-1",
            "id": "test-meeting-1",
            "host_id": "test-host-1",
            "topic": "Test Meeting 1",
            "type": 2,
            "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
            "duration": 60,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

sleep 2
verify_api "$API_ENDPOINT" 1 "meeting created"

# Test 2: Start the meeting
echo -e "\n${BLUE}Test 2: Start the meeting${NC}"
send_event "meeting.started" '{
    "event": "meeting.started",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "test-uuid-1",
            "id": "test-meeting-1",
            "host_id": "test-host-1",
            "topic": "Test Meeting 1",
            "type": 2
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

sleep 2
# Still should be 1 meeting
verify_api "$API_ENDPOINT" 1 "meeting started"

# Test 3: Add participants
echo -e "\n${BLUE}Test 3: Add participants${NC}"
for i in {1..3}; do
    send_event "meeting.participant_joined" '{
        "event": "meeting.participant_joined",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "test-uuid-1",
                "id": "test-meeting-1",
                "host_id": "test-host-1",
                "participant": {
                    "id": "test-participant-'$i'",
                    "user_id": "test-user-'$i'",
                    "user_name": "Test User '$i'",
                    "email": "test'$i'@example.com",
                    "join_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                }
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    sleep 1
done

# Test 4: Remove a participant
echo -e "\n${BLUE}Test 4: Remove a participant${NC}"
send_event "meeting.participant_left" '{
    "event": "meeting.participant_left",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "test-uuid-1",
            "id": "test-meeting-1",
            "host_id": "test-host-1",
            "participant": {
                "id": "test-participant-1",
                "user_id": "test-user-1",
                "user_name": "Test User 1",
                "email": "test1@example.com",
                "leave_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
            }
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

sleep 2

# Test 5: End the meeting
echo -e "\n${BLUE}Test 5: End the meeting${NC}"
send_event "meeting.ended" '{
    "event": "meeting.ended",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "test-uuid-1",
            "id": "test-meeting-1",
            "host_id": "test-host-1",
            "topic": "Test Meeting 1",
            "type": 2
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

sleep 2
# Meeting should still exist but be marked as ended
verify_api "$API_ENDPOINT?include_ended=true" 1 "meeting ended"

# Test 6: URL validation challenge
echo -e "\n${BLUE}Test 6: URL validation challenge${NC}"
send_event "endpoint.url_validation" '{
    "event": "endpoint.url_validation",
    "payload": {
        "plainToken": "testtoken12345"
    },
    "event_ts": '"$(date +%s)"'000
}'

# Test 7: Concurrent meetings
echo -e "\n${BLUE}Test 7: Create multiple concurrent meetings${NC}"
for i in {1..3}; do
    send_event "meeting.created" '{
        "event": "meeting.created",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "test-uuid-'$i'",
                "id": "concurrent-meeting-'$i'",
                "host_id": "test-host-'$i'",
                "topic": "Concurrent Meeting '$i'",
                "type": 2,
                "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
                "duration": 60,
                "timezone": "UTC"
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    
    sleep 1
    
    send_event "meeting.started" '{
        "event": "meeting.started",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "test-uuid-'$i'",
                "id": "concurrent-meeting-'$i'",
                "host_id": "test-host-'$i'",
                "topic": "Concurrent Meeting '$i'",
                "type": 2
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    
    sleep 1
}

sleep 2
verify_api "$API_ENDPOINT" 3 "concurrent meetings"

# Print test summary
echo -e "\n${BLUE}Test Summary${NC}"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${RED}Failed: $FAILED${NC}"

if [ "$FAILED" -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed!${NC}"
    exit 1
fi
