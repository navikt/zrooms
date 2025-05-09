// app.js - Client-side functionality for ZRooms

document.addEventListener('DOMContentLoaded', function() {
    // Set up auto-refresh counter if SSE is not enabled
    if (!useSSE) {
        setupRefreshCounter();
    } else {
        setupCustomSSE();
    }
    
    // Apply alternating row colors
    applyRowStyles();
    
    // Set up meeting count in page title
    updateMeetingCountInTitle();
});

// Sets up a custom Server-Sent Events implementation using Fetch API
// This approach avoids QUIC protocol issues in Chrome by forcing HTTP/1.1
function setupCustomSSE() {
    // Create status elements
    const statusElem = document.createElement('div');
    statusElem.id = 'sse-status';
    statusElem.className = 'sse-status';
    statusElem.textContent = 'Connecting...';
    document.body.appendChild(statusElem);
    
    const flashElem = document.createElement('div');
    flashElem.id = 'update-flash';
    flashElem.className = 'update-flash';
    document.body.appendChild(flashElem);
    
    // Custom SSE implementation variables
    let abortController = null;
    let reconnectAttempt = 0;
    let reconnectTimer = null;
    const maxRetries = 5;
    
    // Process SSE text chunks
    function processEventStream(text, lastEventId = '') {
        // Split the text into individual event chunks
        const eventChunks = text.split('\n\n');
        let newLastEventId = lastEventId;
        
        for (const chunk of eventChunks) {
            if (!chunk.trim()) continue;
            
            const lines = chunk.split('\n');
            let eventType = 'message';
            let data = '';
            let id = '';
            
            for (const line of lines) {
                if (line.startsWith('event:')) {
                    eventType = line.substring(6).trim();
                } else if (line.startsWith('data:')) {
                    data = line.substring(5).trim();
                } else if (line.startsWith('id:')) {
                    id = line.substring(3).trim();
                    newLastEventId = id;
                } else if (line.startsWith(':')) {
                    // This is a comment, used for keep-alive
                    continue;
                }
            }
            
            if (data) {
                // Create and dispatch a custom event
                const event = new CustomEvent(eventType, {
                    detail: { data, id }
                });
                
                // Handle different event types
                if (eventType === 'connected') {
                    try {
                        const parsedData = JSON.parse(data);
                        console.log('SSE connected, client ID:', parsedData.id);
                        statusElem.textContent = 'Live updates: Connected';
                        statusElem.className = 'sse-status connected';
                        reconnectAttempt = 0;
                    } catch (e) {
                        console.error('Error handling connected event:', e);
                    }
                } else if (eventType === 'update') {
                    try {
                        const meetings = JSON.parse(data);
                        updateMeetingsTable(meetings);
                        
                        // Update timestamp
                        const now = new Date();
                        const timeString = now.toLocaleTimeString();
                        const lastUpdatedElem = document.getElementById('last-updated');
                        if (lastUpdatedElem) {
                            lastUpdatedElem.textContent = timeString;
                        }
                        
                        showUpdateFlash();
                    } catch (e) {
                        console.error('Error handling update event:', e);
                    }
                }
            }
        }
        
        return newLastEventId;
    }
    
    // Custom SSE connection function using Fetch API
    async function connectSSE() {
        // Clear any existing reconnect timer
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
            reconnectTimer = null;
        }
        
        // Create a new abort controller for this connection
        abortController = new AbortController();
        const signal = abortController.signal;
        
        // Track the last event ID to resume streaming if disconnected
        let lastEventId = '';
        let textBuffer = '';
        
        try {
            // Start a fetch request with the right headers to get SSE stream
            const response = await fetch(`/events?t=${Date.now()}`, {
                method: 'GET',
                headers: {
                    'Accept': 'text/event-stream',
                    'Cache-Control': 'no-cache',
                    // Last-Event-ID header helps resume streaming after disconnection
                    ...(lastEventId ? { 'Last-Event-ID': lastEventId } : {})
                },
                // Force HTTP/1.1 to avoid QUIC protocol issues
                cache: 'no-store',
                // Don't convert the response to JSON automatically
                // We need to process it as a text stream
                signal: signal
            });
            
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            
            // This is key - we need to get the reader to process the stream
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            
            console.log('SSE fetch connection established');
            
            // Process the stream
            while (true) {
                const { value, done } = await reader.read();
                
                if (done) {
                    console.log('SSE stream complete');
                    break;
                }
                
                // Decode the chunk and add it to our buffer
                const chunk = decoder.decode(value, { stream: true });
                textBuffer += chunk;
                
                // Process complete events (ending with double newlines)
                if (textBuffer.includes('\n\n')) {
                    lastEventId = processEventStream(textBuffer, lastEventId);
                    
                    // Keep only the incomplete part (after the last double newline)
                    const lastIndex = textBuffer.lastIndexOf('\n\n');
                    if (lastIndex !== -1) {
                        textBuffer = textBuffer.substring(lastIndex + 2);
                    }
                }
            }
        } catch (error) {
            // Don't log AbortError which happens during normal disconnection
            if (error.name !== 'AbortError') {
                console.error('SSE fetch error:', error);
            }
            
            statusElem.textContent = 'Live updates: Disconnected';
            statusElem.className = 'sse-status disconnected';
            
            // Implement reconnection with exponential backoff
            reconnectAttempt++;
            const delay = Math.min(reconnectAttempt * 2000, 10000);
            
            console.log(`Reconnecting in ${delay/1000} seconds... (attempt ${reconnectAttempt})`);
            
            // Set up reconnection timer
            reconnectTimer = setTimeout(() => {
                if (reconnectAttempt < maxRetries) {
                    connectSSE();
                } else {
                    console.log('Maximum reconnection attempts reached, falling back to refresh');
                    statusElem.textContent = 'Live updates: Failed';
                    statusElem.className = 'sse-status error';
                    setupRefreshCounter();
                }
            }, delay);
        }
    }
    
    // Start the initial connection
    connectSSE();
    
    // Clean up on page unload
    window.addEventListener('beforeunload', function() {
        if (abortController) {
            abortController.abort();
        }
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
        }
    });
}

// Update the meetings table with new data
function updateMeetingsTable(meetings) {
    const tableBody = document.querySelector('table tbody');
    if (!tableBody) return;
    
    // Clear existing rows
    tableBody.innerHTML = '';
    
    // Check if we have meetings
    if (!meetings || meetings.length === 0) {
        const emptyRow = document.createElement('tr');
        emptyRow.innerHTML = `<td colspan="4" class="no-meetings">No active meetings</td>`;
        tableBody.appendChild(emptyRow);
    } else {
        // Add rows for each meeting
        meetings.forEach(function(meeting) {
            if (!meeting) return; // Skip invalid meetings
            
            const row = document.createElement('tr');
            
            // Safely access properties with defaults
            const meetingObj = meeting.meeting || {};
            const topic = meetingObj.topic || 'Unnamed Meeting';
            const participantCount = meeting.participantCount || 0;
            const startedAt = meeting.startedAt || '';
            
            // Determine status class
            let statusClass = '';
            let statusText = 'Scheduled';
            
            if (meeting.status === 'in_progress') {
                statusClass = 'meeting-active';
                statusText = 'In Progress';
            } else if (meeting.status === 'ended') {
                statusClass = 'meeting-ended';
                statusText = 'Ended';
            }
            
            // Format the meeting data
            row.innerHTML = `
                <td>${topic}</td>
                <td class="${statusClass}">${statusText}</td>
                <td class="center">${participantCount}</td>
                <td>${formatTime(startedAt)}</td>
            `;
            
            tableBody.appendChild(row);
        });
    }
    
    // Re-apply alternating row styles
    applyRowStyles();
    
    // Update meeting count in title
    updateMeetingCountInTitle();
}

// Show a flash effect when updates occur
function showUpdateFlash() {
    const flash = document.getElementById('update-flash');
    if (!flash) return;
    
    // Set opacity to show the flash
    flash.style.opacity = '1';
    
    // Fade out after a short delay
    setTimeout(function() {
        flash.style.opacity = '0';
    }, 200);
}

// Format a time string
function formatTime(timeString) {
    if (!timeString) return '-';
    
    try {
        const date = new Date(timeString);
        if (isNaN(date.getTime())) return '-';
        return date.toLocaleTimeString();
    } catch (e) {
        return '-';
    }
}

// Apply alternating row styles
function applyRowStyles() {
    const rows = document.querySelectorAll('tbody tr');
    rows.forEach((row, i) => {
        if (i % 2 !== 0) {
            row.style.backgroundColor = 'rgba(0, 0, 0, 0.02)';
        }
    });
}

// Updates the meeting count in the browser title
function updateMeetingCountInTitle() {
    const meetingRows = document.querySelectorAll('tbody tr');
    const noMeetingsRow = document.querySelector('tbody tr td.no-meetings');
    
    // If there's a "No meetings" row, don't count it
    const count = noMeetingsRow ? 0 : meetingRows.length;
    
    if (count > 0) {
        document.title = `(${count}) ZRooms - Meeting Status`;
    } else {
        document.title = `ZRooms - Meeting Status`;
    }
}

// Sets up a countdown timer for the auto-refresh
function setupRefreshCounter() {
    // Get the refresh interval from the meta tag
    const refreshMeta = document.querySelector('meta[http-equiv="refresh"]');
    if (!refreshMeta) return;
    
    const content = refreshMeta.getAttribute('content');
    let seconds = parseInt(content, 10);
    if (isNaN(seconds)) return;
    
    // Create or get the refresh counter element
    let counterElem = document.getElementById('refresh-counter');
    if (!counterElem) {
        counterElem = document.createElement('div');
        counterElem.id = 'refresh-counter';
        counterElem.style.position = 'fixed';
        counterElem.style.bottom = '10px';
        counterElem.style.right = '10px';
        counterElem.style.backgroundColor = 'rgba(0, 0, 0, 0.7)';
        counterElem.style.color = 'white';
        counterElem.style.padding = '5px 10px';
        counterElem.style.borderRadius = '3px';
        counterElem.style.fontSize = '12px';
        document.body.appendChild(counterElem);
    }
    
    // Update the counter every second
    const interval = setInterval(() => {
        seconds--;
        counterElem.textContent = `Refreshing in ${seconds}s`;
        
        if (seconds <= 0) {
            clearInterval(interval);
        }
    }, 1000);
}