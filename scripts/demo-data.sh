#!/bin/bash

# Demo script for Zrooms - Webhook-Only Architecture
# This script sends webhook events to simulate realistic Zoom meeting activity
# Supports both one-time demo and continuous simulation modes
#
# SIMPLIFIED VERSION:
# - No complex state tracking (the Go application handles that)
# - Uses predefined meeting IDs for simplicity
# - Focuses on generating realistic webhook events
# - Compatible with macOS bash (no associative arrays)
#
# Usage:
#   ./demo-data.sh          # One-time demo setup
#   ./demo-data.sh -c       # Continuous simulation

# Configuration
HOST="http://localhost:8080"
WEBHOOK_ENDPOINT="/webhook"
HEADER="Content-Type: application/json"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Global variables for generating unique IDs
MEETING_COUNTER=1
PARTICIPANT_COUNTER=1

# Meeting topics pool
TOPICS=(
    "Weekly Team Standup"
    "Product Planning Session"
    "Customer Demo"
    "Sprint Review"
    "Architecture Discussion"
    "Marketing Strategy"
    "Sales Pipeline Review"
    "Engineering Sync"
    "Design Review"
    "Q&A Session"
    "Board Meeting"
    "Training Session"
    "Project Kickoff"
    "Retrospective"
    "Client Consultation"
)

# Display intro message
echo -e "${BLUE}╔══════════════════════════════════════╗${NC}"
echo -e "${BLUE}║        Zrooms Demo Generator         ║${NC}"
echo -e "${BLUE}║     Webhook-Only Architecture        ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════╝${NC}"
echo -e "Make sure your application is running at ${HOST}\n"

# Utility function to send webhook events
send_event() {
    local event_type=$1
    local payload=$2

    echo -e "${YELLOW}📡 Sending ${event_type} event...${NC}"

    response=$(curl -s -w "%{http_code}" -X POST -H "${HEADER}" -d "${payload}" "${HOST}${WEBHOOK_ENDPOINT}")
    http_code="${response: -3}"

    if [ "$http_code" -eq 200 ]; then
        echo -e "${GREEN}✓ Event sent successfully${NC}"
    else
        echo -e "${RED}✗ Failed to send event (HTTP: $http_code)${NC}"
    fi

    # Brief delay between events
    sleep 0.5
}

# Function to get a random topic
get_random_topic() {
    echo "${TOPICS[$((RANDOM % ${#TOPICS[@]}))]}"
}

# Function to generate a random participant name
get_random_participant() {
    local names=("Alice" "Bob" "Charlie" "Diana" "Eve" "Frank" "Grace" "Henry" "Iris" "Jack" "Kate" "Liam" "Maya" "Noah" "Olivia" "Peter" "Quinn" "Ruby" "Sam" "Tina")
    local surname=("Smith" "Johnson" "Williams" "Brown" "Jones" "Garcia" "Miller" "Davis" "Rodriguez" "Martinez")

    local first_name="${names[$((RANDOM % ${#names[@]}))]}"
    local last_name="${surname[$((RANDOM % ${#surname[@]}))]}"
    echo "${first_name} ${last_name}"
}

# Function to create and start a meeting
create_and_start_meeting() {
    local meeting_id="meeting-${MEETING_COUNTER}"
    local topic=$(get_random_topic)
    local host_id="host-${MEETING_COUNTER}"

    echo -e "\n${CYAN}🚀 Creating and starting meeting: ${topic}${NC}"

    # Create meeting
    send_event "meeting.created" '{
        "event": "meeting.created",
        "payload": {
            "account_id": "demo-account",
            "object": {
                "uuid": "uuid-'"${MEETING_COUNTER}"'",
                "id": "'"${meeting_id}"'",
                "host_id": "'"${host_id}"'",
                "topic": "'"${topic}"'",
                "type": 2,
                "start_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'",
                "duration": '$((30 + RANDOM % 90))',
                "timezone": "UTC"
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'

    sleep 1

    # Start meeting
    send_event "meeting.started" '{
        "event": "meeting.started",
        "payload": {
            "account_id": "demo-account",
            "object": {
                "uuid": "uuid-'"${MEETING_COUNTER}"'",
                "id": "'"${meeting_id}"'",
                "host_id": "'"${host_id}"'",
                "topic": "'"${topic}"'",
                "type": 2,
                "duration": '$((30 + RANDOM % 90))',
                "timezone": "UTC"
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'

    MEETING_COUNTER=$((MEETING_COUNTER + 1))
    return 0
}

# Function to add participants to a meeting
add_participants_to_meeting() {
    local meeting_id=$1
    local num_participants=$2

    echo -e "${BLUE}👥 Adding ${num_participants} participants to meeting ${meeting_id}${NC}"

    for ((i=1; i<=num_participants; i++)); do
        local participant_name=$(get_random_participant)
        local participant_id="participant-${PARTICIPANT_COUNTER}"
        local user_id="user-${PARTICIPANT_COUNTER}"
        local email="${participant_name// /.}"
        email=$(echo "$email" | tr '[:upper:]' '[:lower:]')@example.com

        send_event "meeting.participant_joined" '{
            "event": "meeting.participant_joined",
            "payload": {
                "account_id": "demo-account",
                "object": {
                    "uuid": "uuid-'"${meeting_id#meeting-}"'",
                    "id": "'"${meeting_id}"'",
                    "host_id": "host-'"${meeting_id#meeting-}"'",
                    "participant": {
                        "id": "'"${participant_id}"'",
                        "user_id": "'"${user_id}"'",
                        "user_name": "'"${participant_name}"'",
                        "email": "'"${email}"'",
                        "join_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                    }
                }
            },
            "event_ts": '"$(date +%s)"'000
        }'

        PARTICIPANT_COUNTER=$((PARTICIPANT_COUNTER + 1))

        # Random delay between participants
        sleep $((1 + RANDOM % 3))
    done
}

# Function to remove random participants from a meeting
remove_participants_from_meeting() {
    local meeting_id=$1
    local num_to_remove=$2

    echo -e "${YELLOW}👋 ${num_to_remove} participants leaving meeting ${meeting_id}${NC}"

    for ((i=1; i<=num_to_remove; i++)); do
        local participant_name=$(get_random_participant)
        local participant_id="participant-$((PARTICIPANT_COUNTER - RANDOM % 50))"
        local user_id="user-$((PARTICIPANT_COUNTER - RANDOM % 50))"
        local email="${participant_name// /.}"
        email=$(echo "$email" | tr '[:upper:]' '[:lower:]')@example.com

        send_event "meeting.participant_left" '{
            "event": "meeting.participant_left",
            "payload": {
                "account_id": "demo-account",
                "object": {
                    "uuid": "uuid-'"${meeting_id#meeting-}"'",
                    "id": "'"${meeting_id}"'",
                    "host_id": "host-'"${meeting_id#meeting-}"'",
                    "participant": {
                        "id": "'"${participant_id}"'",
                        "user_id": "'"${user_id}"'",
                        "user_name": "'"${participant_name}"'",
                        "email": "'"${email}"'",
                        "leave_time": "'"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"'"
                    }
                }
            },
            "event_ts": '"$(date +%s)"'000
        }'

        # Random delay between participants leaving
        sleep $((1 + RANDOM % 2))
    done
}

# Function to end a meeting
end_meeting() {
    local meeting_id=$1

    echo -e "${RED}🔚 Ending meeting: ${meeting_id}${NC}"

    send_event "meeting.ended" '{
        "event": "meeting.ended",
        "payload": {
            "account_id": "demo-account",
            "object": {
                "uuid": "uuid-'"${meeting_id#meeting-}"'",
                "id": "'"${meeting_id}"'",
                "host_id": "host-'"${meeting_id#meeting-}"'",
                "topic": "Demo Meeting",
                "type": 2
            }
        },
        "event_ts": '"$(date +%s)"'000
    }'
}

# Function to run initial demo (one-time setup)
run_initial_demo() {
    echo -e "\n${BLUE}🎬 Running Initial Demo Setup${NC}"

    # Create and start 3 initial meetings
    create_and_start_meeting
    sleep 2
    add_participants_to_meeting "meeting-1" $((3 + RANDOM % 5))

    create_and_start_meeting
    sleep 2
    add_participants_to_meeting "meeting-2" $((2 + RANDOM % 4))

    create_and_start_meeting
    sleep 2
    add_participants_to_meeting "meeting-3" $((4 + RANDOM % 6))

    echo -e "\n${GREEN}✅ Initial demo setup complete!${NC}"
    echo -e "${CYAN}Check the web interface at ${HOST} to see the meetings${NC}"
}

# Function to simulate ongoing activity
simulate_activity() {
    echo -e "\n${BLUE}🔄 Starting continuous simulation...${NC}"
    echo -e "${YELLOW}Press Ctrl+C to stop${NC}\n"

    local iteration=1
    local active_meetings=("meeting-1" "meeting-2" "meeting-3")

    while true; do
        echo -e "${CYAN}--- Simulation Iteration ${iteration} ---${NC}"

        # Random action
        local action=$((RANDOM % 10))
        local meeting_id="${active_meetings[$((RANDOM % ${#active_meetings[@]}))]}"

        case $action in
            0|1|2) # 30% chance: Add participants
                add_participants_to_meeting "$meeting_id" $((1 + RANDOM % 3))
                ;;
            3|4) # 20% chance: Remove participants
                remove_participants_from_meeting "$meeting_id" $((1 + RANDOM % 2))
                ;;
            5) # 10% chance: End and restart a meeting
                end_meeting "$meeting_id"
                sleep 2
                create_and_start_meeting
                sleep 2
                add_participants_to_meeting "$meeting_id" $((1 + RANDOM % 4))
                ;;
            6|7) # 20% chance: Create additional meeting
                local new_meeting_id="meeting-$((MEETING_COUNTER))"
                create_and_start_meeting
                sleep 2
                add_participants_to_meeting "$new_meeting_id" $((1 + RANDOM % 4))
                active_meetings+=("$new_meeting_id")
                ;;
            *) # 20% chance: Just status update
                echo -e "${BLUE}📊 Status check - ${#active_meetings[@]} meetings being simulated${NC}"
                ;;
        esac

        # Wait between actions (5-15 seconds)
        local wait_time=$((5 + RANDOM % 10))
        echo -e "${YELLOW}⏳ Waiting ${wait_time} seconds...${NC}\n"
        sleep $wait_time

        iteration=$((iteration + 1))
    done
}

# Main execution logic
main() {
    # Parse command line arguments
    MODE="demo"

    while [[ $# -gt 0 ]]; do
        case $1 in
            --continuous|-c)
                MODE="continuous"
                shift
                ;;
            --help|-h)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Options:"
                echo "  --continuous, -c    Run continuous simulation (default: one-time demo)"
                echo "  --help, -h         Show this help message"
                echo ""
                echo "Examples:"
                echo "  $0                 # Run one-time demo"
                echo "  $0 --continuous    # Run continuous simulation"
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                echo "Use --help for usage information"
                exit 1
                ;;
        esac
    done

    # Health check
    echo -e "${BLUE}🔍 Checking application health...${NC}"
    if ! curl -s "${HOST}/health/live" > /dev/null; then
        echo -e "${RED}❌ Application not running at ${HOST}${NC}"
        echo -e "Please start the Zrooms application first"
        exit 1
    fi
    echo -e "${GREEN}✅ Application is running${NC}"

    # Run based on mode
    if [ "$MODE" = "continuous" ]; then
        run_initial_demo
        sleep 3
        simulate_activity
    else
        run_initial_demo
        echo -e "\n${GREEN}🎉 Demo completed!${NC}"
        echo -e "${CYAN}💡 Use -c flag for ongoing simulation${NC}"
    fi
}

# Cleanup function for graceful shutdown
cleanup() {
    echo -e "\n\n${YELLOW}🛑 Shutting down simulation...${NC}"

    # End the main demo meetings
    for meeting_id in "meeting-1" "meeting-2" "meeting-3"; do
        end_meeting "$meeting_id"
        sleep 1
    done

    echo -e "${GREEN}✅ Cleanup complete${NC}"
    exit 0
}

# Set up signal handlers
trap cleanup SIGINT SIGTERM

# Run the main function
main "$@"
