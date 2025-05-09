// Simplified app.js - Client-side functionality for ZRooms with debug-friendly SSE

document.addEventListener('DOMContentLoaded', function() {
    // Set up auto-refresh counter if SSE is not enabled
    if (!useSSE) {
        setupRefreshCounter();
    } else {
        setupDebugFriendlySSE();
    }
    
    // Apply alternating row colors
    applyRowStyles();
    
    // Set up meeting count in page title
    updateMeetingCountInTitle();
});

// Sets up a simplified, debug-friendly Server-Sent Events implementation
function setupDebugFriendlySSE() {
    // Create status elements for debugging
    const statusElem = document.createElement('div');
    statusElem.id = 'sse-status';
    statusElem.className = 'sse-status';
    statusElem.textContent = 'Connecting...';
    document.body.appendChild(statusElem);
    
    const debugElem = document.createElement('div');
    debugElem.id = 'sse-debug';
    debugElem.style.position = 'fixed';
    debugElem.style.bottom = '50px';
    debugElem.style.right = '10px';
    debugElem.style.backgroundColor = 'rgba(0, 0, 0, 0.7)';
    debugElem.style.color = 'white';
    debugElem.style.padding = '10px';
    debugElem.style.borderRadius = '5px';
    debugElem.style.maxWidth = '400px';
    debugElem.style.maxHeight = '200px';
    debugElem.style.overflow = 'auto';
    debugElem.style.fontSize = '12px';
    debugElem.style.fontFamily = 'monospace';
    document.body.appendChild(debugElem);
    
    const flashElem = document.createElement('div');
    flashElem.id = 'update-flash';
    flashElem.className = 'update-flash';
    document.body.appendChild(flashElem);
    
    // Helper function to log debug messages
    function logDebug(message) {
        console.log(message);
        const line = document.createElement('div');
        line.textContent = `[${new Date().toLocaleTimeString()}] ${message}`;
        debugElem.appendChild(line);
        debugElem.scrollTop = debugElem.scrollHeight;
        
        // Keep only last 20 lines
        while (debugElem.childElementCount > 20) {
            debugElem.removeChild(debugElem.firstChild);
        }
    }
    
    // Try simple fetch-based SSE first
    function connectWithFetch() {
        logDebug('Trying fetch-based SSE connection...');
        statusElem.textContent = 'Connecting via Fetch...';
        
        const url = new URL('/events', window.location.href);
        url.searchParams.set('t', Date.now().toString());
        
        logDebug(`Connecting to: ${url.toString()}`);
        
        fetch(url.toString(), {
            method: 'GET',
            headers: {
                'Accept': 'text/event-stream',
                'Cache-Control': 'no-cache'
            },
            cache: 'no-store'
        })
        .then(response => {
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            
            logDebug('Fetch connection established');
            statusElem.textContent = 'Connected via Fetch';
            statusElem.className = 'sse-status connected';
            
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            
            function readChunk() {
                reader.read().then(({ value, done }) => {
                    if (done) {
                        logDebug('Stream complete');
                        return;
                    }
                    
                    // Process the chunk
                    const chunk = decoder.decode(value, { stream: true });
                    buffer += chunk;
                    
                    logDebug(`Received chunk: ${chunk.length} bytes`);
                    
                    // Process complete events
                    if (buffer.includes('\n\n')) {
                        processEvents(buffer);
                        
                        // Keep only the incomplete part
                        const lastIndex = buffer.lastIndexOf('\n\n');
                        if (lastIndex !== -1) {
                            buffer = buffer.substring(lastIndex + 2);
                        }
                    }
                    
                    // Continue reading
                    readChunk();
                }).catch(error => {
                    logDebug(`Error reading: ${error.message}`);
                    statusElem.textContent = 'Disconnected (fetch error)';
                    statusElem.className = 'sse-status disconnected';
                    
                    // Fallback to EventSource after a delay
                    setTimeout(connectWithEventSource, 2000);
                });
            }
            
            // Start reading the stream
            readChunk();
        })
        .catch(error => {
            logDebug(`Fetch failed: ${error.message}`);
            statusElem.textContent = 'Fetch connection failed';
            statusElem.className = 'sse-status error';
            
            // Fallback to EventSource
            setTimeout(connectWithEventSource, 1000);
        });
    }
    
    // Try with EventSource as fallback
    function connectWithEventSource() {
        logDebug('Trying EventSource connection...');
        statusElem.textContent = 'Connecting via EventSource...';
        
        // Close any existing EventSource
        if (window.eventSource) {
            window.eventSource.close();
        }
        
        try {
            const url = new URL('/events', window.location.href);
            url.searchParams.set('t', Date.now().toString());
            
            logDebug(`EventSource URL: ${url.toString()}`);
            
            const source = new EventSource(url.toString());
            window.eventSource = source;
            
            source.onopen = function() {
                logDebug('EventSource connection opened');
                statusElem.textContent = 'Connected via EventSource';
                statusElem.className = 'sse-status connected';
            };
            
            source.onerror = function(error) {
                logDebug(`EventSource error: ${error.type}`);
                source.close();
                
                statusElem.textContent = 'EventSource connection failed';
                statusElem.className = 'sse-status error';
                
                // Add fallback to manual refresh
                logDebug('Falling back to page refresh');
                setupRefreshCounter();
            };
            
            source.addEventListener('connected', function(e) {
                try {
                    const data = JSON.parse(e.data);
                    logDebug(`Connected as client ID: ${data.id}`);
                } catch (err) {
                    logDebug(`Error parsing connected event: ${err.message}`);
                }
            });
            
            source.addEventListener('update', function(e) {
                logDebug('Received update event');
                try {
                    const meetings = JSON.parse(e.data);
                    logDebug(`Parsed ${meetings.length} meetings`);
                    updateMeetingsTable(meetings);
                    showUpdateFlash();
                    
                    // Update timestamp
                    const now = new Date();
                    const timeString = now.toLocaleTimeString();
                    const lastUpdatedElem = document.getElementById('last-updated');
                    if (lastUpdatedElem) {
                        lastUpdatedElem.textContent = timeString;
                    }
                } catch (err) {
                    logDebug(`Error processing update: ${err.message}`);
                }
            });
        } catch (e) {
            logDebug(`EventSource setup failed: ${e.message}`);
            setupRefreshCounter();
        }
    }
    
    // Process events from fetch-based approach
    function processEvents(text) {
        logDebug(`Processing events: ${text.length} bytes`);
        
        // Parse the SSE format and extract events
        const events = text.split('\n\n').filter(chunk => chunk.trim());
        
        events.forEach(eventText => {
            logDebug(`Event chunk: ${eventText.substr(0, 50)}...`);
            
            // Parse the event
            const lines = eventText.split('\n');
            let eventType = null;
            let data = null;
            
            lines.forEach(line => {
                if (line.startsWith('event:')) {
                    eventType = line.substring(6).trim();
                } else if (line.startsWith('data:')) {
                    data = line.substring(5).trim();
                }
            });
            
            if (!eventType || !data) {
                logDebug('Incomplete event, skipping');
                return;
            }
            
            logDebug(`Parsed event type: ${eventType}`);
            
            if (eventType === 'connected') {
                try {
                    const parsedData = JSON.parse(data);
                    logDebug(`Connected as client ID: ${parsedData.id}`);
                } catch (e) {
                    logDebug(`Error parsing connected data: ${e.message}`);
                }
            } else if (eventType === 'update') {
                try {
                    const meetings = JSON.parse(data);
                    logDebug(`Parsed ${meetings.length} meetings`);
                    updateMeetingsTable(meetings);
                    showUpdateFlash();
                    
                    // Update timestamp
                    const now = new Date();
                    const timeString = now.toLocaleTimeString();
                    const lastUpdatedElem = document.getElementById('last-updated');
                    if (lastUpdatedElem) {
                        lastUpdatedElem.textContent = timeString;
                    }
                } catch (e) {
                    logDebug(`Error parsing update data: ${e.message}`);
                }
            }
        });
    }
    
    // Start with fetch approach first (more reliable across browsers)
    connectWithFetch();
    
    // Add button to toggle debug panel
    const toggleBtn = document.createElement('button');
    toggleBtn.textContent = 'Toggle Debug';
    toggleBtn.style.position = 'fixed';
    toggleBtn.style.bottom = '10px';
    toggleBtn.style.right = '10px';
    toggleBtn.style.zIndex = '1000';
    toggleBtn.onclick = function() {
        debugElem.style.display = debugElem.style.display === 'none' ? 'block' : 'none';
    };
    document.body.appendChild(toggleBtn);
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