#!/bin/bash

# Demo script for Zrooms
# This script sends test data to a running Zrooms application to simulate
# Zoom webhook events for demonstration purposes.

# Configuration
HOST="http://localhost:8080"
WEBHOOK_ENDPOINT="/webhook"
HEADER="Content-Type: application/json"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Display intro message
echo -e "${BLUE}Zrooms Demo Data Generator${NC}"
echo -e "This script will populate your Zrooms application with test data."
echo -e "Make sure your application is running at ${HOST}\n"

# Utility function to send webhook events
send_event() {
    local event_type=$1
    local payload=$2
    
    echo -e "${YELLOW}Sending ${event_type} event...${NC}"
    
    curl -s -X POST -H "${HEADER}" -d "${payload}" "${HOST}${WEBHOOK_ENDPOINT}" > /dev/null
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Event sent successfully${NC}"
    else
        echo -e "${RED}✗ Failed to send event${NC}"
    fi
    
    # Give the application time to process the event
    sleep 1
}

# Function to create a room
create_room() {
    local room_id=$1
    local room_name=$2
    local capacity=$3
    local location=$4
    
    echo -e "\n${BLUE}Creating Room: ${room_name}${NC}"
    
    # Direct API call to create a room
    curl -s -X POST -H "${HEADER}" -d '{
        "id": "'"${room_id}"'",
        "name": "'"${room_name}"'",
        "capacity": '"${capacity}"',
        "location": "'"${location}"'"
    }' "${HOST}/api/rooms" > /dev/null
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Room created successfully${NC}"
    else
        echo -e "${RED}✗ Failed to create room${NC}"
    fi
    
    sleep 1
}

# 1. Create rooms
echo -e "\n${BLUE}Step 1: Creating Rooms${NC}"
create_room "room-1" "Executive Boardroom" 20 "Floor 1"
create_room "room-2" "Marketing Conference Room" 15 "Floor 2"
create_room "room-3" "Engineering Conference Room" 12 "Floor 3"
create_room "room-4" "Small Meeting Room" 5 "Floor 2"

# 2. Create and start meetings
echo -e "\n${BLUE}Step 2: Creating Meetings${NC}"

# Meeting 1: Executive Boardroom
echo -e "\n${BLUE}Creating Meeting: Board Meeting in Executive Boardroom${NC}"
send_event "meeting.created" '{
    "event": "meeting.created",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid1",
            "id": "meeting-1",
            "host_id": "host-1",
            "topic": "Q2 Board Meeting",
            "type": 2,
            "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
            "duration": 60,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Associate meeting with room
curl -s -X PUT -H "${HEADER}" -d '{
    "meeting_id": "meeting-1",
    "room_id": "room-1"
}' "${HOST}/api/rooms/room-1/meetings/meeting-1" > /dev/null

# Meeting 2: Marketing Conference Room
echo -e "\n${BLUE}Creating Meeting: Marketing Strategy in Marketing Conference Room${NC}"
send_event "meeting.created" '{
    "event": "meeting.created",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid2",
            "id": "meeting-2",
            "host_id": "host-2",
            "topic": "Q3 Marketing Strategy",
            "type": 2,
            "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
            "duration": 45,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Associate meeting with room
curl -s -X PUT -H "${HEADER}" -d '{
    "meeting_id": "meeting-2",
    "room_id": "room-2"
}' "${HOST}/api/rooms/room-2/meetings/meeting-2" > /dev/null

# Meeting 3: Engineering Conference Room
echo -e "\n${BLUE}Creating Meeting: Sprint Planning in Engineering Conference Room${NC}"
send_event "meeting.created" '{
    "event": "meeting.created",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid3",
            "id": "meeting-3",
            "host_id": "host-3",
            "topic": "Sprint Planning",
            "type": 2,
            "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
            "duration": 30,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Associate meeting with room
curl -s -X PUT -H "${HEADER}" -d '{
    "meeting_id": "meeting-3",
    "room_id": "room-3"
}' "${HOST}/api/rooms/room-3/meetings/meeting-3" > /dev/null

# 3. Start meetings
echo -e "\n${BLUE}Step 3: Starting Meetings${NC}"

# Start Meeting 1
echo -e "\n${BLUE}Starting Meeting: Board Meeting${NC}"
send_event "meeting.started" '{
    "event": "meeting.started",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid1",
            "id": "meeting-1",
            "host_id": "host-1",
            "topic": "Q2 Board Meeting",
            "type": 2,
            "duration": 60,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Start Meeting 2
echo -e "\n${BLUE}Starting Meeting: Marketing Strategy${NC}"
send_event "meeting.started" '{
    "event": "meeting.started",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid2",
            "id": "meeting-2",
            "host_id": "host-2",
            "topic": "Q3 Marketing Strategy",
            "type": 2,
            "duration": 45,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# 4. Add participants to meetings
echo -e "\n${BLUE}Step 4: Adding Participants to Meetings${NC}"

# Add participants to Meeting 1
for i in {1..6}; do
    echo -e "\n${BLUE}Adding Participant ${i} to Board Meeting${NC}"
    send_event "meeting.participant_joined" '{
        "event": "meeting.participant_joined",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "uuid1",
                "id": "meeting-1",
                "host_id": "host-1",
                "participant": {
                    "id": "'"participant-${i}"'",
                    "user_id": "'"user-${i}"'",
                    "user_name": "Board Member '"${i}"'",
                    "email": "board'"${i}"'@example.com",
                    "join_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                }
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    # Slight delay to make it look realistic
    sleep 0.5
done

# Add participants to Meeting 2
for i in {1..4}; do
    echo -e "\n${BLUE}Adding Participant ${i} to Marketing Strategy Meeting${NC}"
    send_event "meeting.participant_joined" '{
        "event": "meeting.participant_joined",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "uuid2",
                "id": "meeting-2",
                "host_id": "host-2",
                "participant": {
                    "id": "'"marketing-${i}"'",
                    "user_id": "'"mkt-user-${i}"'",
                    "user_name": "Marketing Team '"${i}"'",
                    "email": "marketing'"${i}"'@example.com",
                    "join_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                }
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    # Slight delay to make it look realistic
    sleep 0.5
done

# 5. Start Meeting 3 (Sprint Planning) and add participants
echo -e "\n${BLUE}Starting Meeting: Sprint Planning${NC}"
send_event "meeting.started" '{
    "event": "meeting.started",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid3",
            "id": "meeting-3",
            "host_id": "host-3",
            "topic": "Sprint Planning",
            "type": 2,
            "duration": 30,
            "timezone": "UTC"
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Add participants to Meeting 3
for i in {1..8}; do
    echo -e "\n${BLUE}Adding Participant ${i} to Sprint Planning${NC}"
    send_event "meeting.participant_joined" '{
        "event": "meeting.participant_joined",
        "payload": {
            "account_id": "account123",
            "object": {
                "uuid": "uuid3",
                "id": "meeting-3",
                "host_id": "host-3",
                "participant": {
                    "id": "'"dev-${i}"'",
                    "user_id": "'"dev-user-${i}"'",
                    "user_name": "Developer '"${i}"'",
                    "email": "dev'"${i}"'@example.com",
                    "join_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                }
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
    # Slight delay to make it look realistic
    sleep 0.5
done

# 6. Remove some participants
echo -e "\n${BLUE}Step 5: Some Participants Leaving Meetings${NC}"

# Remove 2 participants from Meeting 1
echo -e "\n${BLUE}Participant 2 leaving Board Meeting${NC}"
send_event "meeting.participant_left" '{
    "event": "meeting.participant_left",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid1",
            "id": "meeting-1",
            "host_id": "host-1",
            "participant": {
                "id": "participant-2",
                "user_id": "user-2",
                "user_name": "Board Member 2",
                "email": "board2@example.com",
                "leave_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
            }
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# Remove 1 participant from Meeting 2
echo -e "\n${BLUE}Participant 3 leaving Marketing Meeting${NC}"
send_event "meeting.participant_left" '{
    "event": "meeting.participant_left",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid2",
            "id": "meeting-2",
            "host_id": "host-2",
            "participant": {
                "id": "marketing-3",
                "user_id": "mkt-user-3",
                "user_name": "Marketing Team 3",
                "email": "marketing3@example.com",
                "leave_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
            }
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

# End Meeting 3 (Sprint Planning)
echo -e "\n${BLUE}Step 6: Ending Sprint Planning Meeting${NC}"
send_event "meeting.ended" '{
    "event": "meeting.ended",
    "payload": {
        "account_id": "account123",
        "object": {
            "uuid": "uuid3",
            "id": "meeting-3",
            "host_id": "host-3",
            "topic": "Sprint Planning",
            "type": 2
        }
    },
    "event_ts": '"$(date +%s)"'000
}'

echo -e "\n${GREEN}Demo data generation complete!${NC}"
echo -e "You should now be able to see meeting rooms with active meetings in your Zrooms web interface."
echo -e "Visit ${HOST} in your web browser to view the results.\n"