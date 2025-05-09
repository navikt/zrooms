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
            // Log connection attempt
            console.log('Attempting SSE connection...');
            
            // Use EventSource as a fallback first - it's more reliable for SSE
            // If this fails, we'll try fetch as a backup
            try {
                const url = new URL('/events', window.location.href);
                url.searchParams.set('t', Date.now().toString());
                
                // Check if we're already using EventSource to prevent double connections
                if (window.eventSource) {
                    window.eventSource.close();
                }
                
                const eventSource = new EventSource(url.toString());
                window.eventSource = eventSource;
                
                console.log('Using native EventSource');
                
                // Set up event handlers
                eventSource.addEventListener('connected', function(event) {
                    try {
                        const parsedData = JSON.parse(event.data);
                        console.log('SSE connected, client ID:', parsedData.id);
                        statusElem.textContent = 'Live updates: Connected';
                        statusElem.className = 'sse-status connected';
                        reconnectAttempt = 0;
                    } catch (e) {
                        console.error('Error handling connected event:', e);
                    }
                });
                
                eventSource.addEventListener('update', function(event) {
                    try {
                        const meetings = JSON.parse(event.data);
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
                });
                
                eventSource.addEventListener('error', function(error) {
                    console.error('EventSource error:', error);
                    eventSource.close();
                    window.eventSource = null;
                    
                    // Fall back to fetch-based approach
                    console.log('Falling back to fetch-based SSE');
                    useFetchBasedSSE();
                });
                
                return; // Exit early if EventSource works
            } catch (e) {
                console.warn('Failed to use EventSource, falling back to fetch:', e);
                // Continue with fetch-based approach
            }
            
            // Fetch-based approach (this is our fallback)
            function useFetchBasedSSE() {
                // Use the same URL structure for consistency
                const url = new URL('/events', window.location.href);
                url.searchParams.set('t', Date.now().toString());
                
                // Try both with and without credentials to work around potential CORS issues
                fetch(url.toString(), {
                    method: 'GET',
                    headers: {
                        'Accept': 'text/event-stream',
                        'Cache-Control': 'no-cache',
                        ...(lastEventId ? { 'Last-Event-ID': lastEventId } : {})
                    },
                    credentials: 'same-origin', // This helps with cookie-based authentication
                    cache: 'no-store',
                    signal: signal
                })
                .then(response => {
                    if (!response.ok) {
                        throw new Error(`HTTP error! status: ${response.status}`);
                    }
                    
                    console.log('SSE fetch connection established');
                    
                    const reader = response.body.getReader();
                    const decoder = new TextDecoder();
                    
                    function readChunk() {
                        reader.read().then(({ value, done }) => {
                            if (done) {
                                console.log('SSE stream complete');
                                return;
                            }
                            
                            // Process the chunk
                            const chunk = decoder.decode(value, { stream: true });
                            textBuffer += chunk;
                            
                            // Process complete events
                            if (textBuffer.includes('\n\n')) {
                                lastEventId = processEventStream(textBuffer, lastEventId);
                                
                                // Keep only the incomplete part
                                const lastIndex = textBuffer.lastIndexOf('\n\n');
                                if (lastIndex !== -1) {
                                    textBuffer = textBuffer.substring(lastIndex + 2);
                                }
                            }
                            
                            // Continue reading
                            readChunk();
                        }).catch(error => {
                            if (error.name !== 'AbortError') {
                                console.error('Error reading SSE chunk:', error);
                                throw error;
                            }
                        });
                    }
                    
                    // Start reading the stream
                    readChunk();
                    
                    return new Promise(() => {}); // This promise never resolves, which is fine for SSE
                })
                .catch(error => {
                    // Handle errors from fetch
                    if (error.name !== 'AbortError') {
                        console.error('SSE fetch error:', error);
                        
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
                });
            }
            
            // Execute the fetch-based approach
            useFetchBasedSSE();
            
        } catch (error) {
            // This catches any errors in the top-level try block
            if (error.name !== 'AbortError') {
                console.error('SSE connection error:', error);
                
                statusElem.textContent = 'Live updates: Disconnected';
                statusElem.className = 'sse-status disconnected';
                
                // Fallback to refresh if we can't establish any kind of SSE connection
                console.log('SSE connection failed, falling back to refresh');
                setupRefreshCounter();
            }
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