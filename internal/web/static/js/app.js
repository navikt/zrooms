// app.js - Client-side functionality for ZRooms

document.addEventListener('DOMContentLoaded', function() {
    // Set up auto-refresh counter if SSE is not enabled
    if (!useSSE) {
        setupRefreshCounter();
    } else {
        setupSSE();
    }
    
    // Apply alternating row colors
    applyRowStyles();
    
    // Set up meeting count in page title
    updateMeetingCountInTitle();
});

// Sets up Server-Sent Events for real-time updates
function setupSSE() {
    // Check if EventSource is supported in this browser
    if (typeof EventSource === 'undefined') {
        console.log('SSE not supported in this browser, falling back to refresh');
        setupRefreshCounter();
        return;
    }
    
    // Create an SSE status indicator
    const statusElem = document.createElement('div');
    statusElem.id = 'sse-status';
    statusElem.className = 'sse-status';
    statusElem.textContent = 'Connecting...';
    document.body.appendChild(statusElem);
    
    // Create a flash element for updates
    const flashElem = document.createElement('div');
    flashElem.id = 'update-flash';
    flashElem.className = 'update-flash';
    document.body.appendChild(flashElem);
    
    // Initialize SSE connection
    const evtSource = new EventSource('/events');
    
    // Connected event
    evtSource.addEventListener('connected', function(event) {
        const data = JSON.parse(event.data);
        console.log('SSE connection established, client ID:', data.id);
        statusElem.textContent = 'Live updates: Connected';
    });
    
    // Update event
    evtSource.addEventListener('update', function(event) {
        // Parse meeting data
        const meetings = JSON.parse(event.data);
        
        // Update the table with new data
        updateMeetingsTable(meetings);
        
        // Update the last updated timestamp
        const now = new Date();
        const timeString = now.toLocaleTimeString();
        const lastUpdatedElem = document.getElementById('last-updated');
        if (lastUpdatedElem) {
            lastUpdatedElem.textContent = timeString;
        }
        
        // Show a flash effect
        showUpdateFlash();
    });
    
    // Error handling
    evtSource.onerror = function(error) {
        console.error('SSE error:', error);
        statusElem.textContent = 'Live updates: Disconnected';
        statusElem.className = 'sse-status disconnected';
        
        // Try to reconnect after a short delay
        setTimeout(function() {
            evtSource.close();
            setupSSE();
        }, 5000);
    };
    
    // Clean up on page unload
    window.addEventListener('beforeunload', function() {
        evtSource.close();
    });
}

// Update the meetings table with new data
function updateMeetingsTable(meetings) {
    const tableBody = document.querySelector('table tbody');
    if (!tableBody) return;
    
    // Clear existing rows
    tableBody.innerHTML = '';
    
    // Check if we have meetings
    if (meetings.length === 0) {
        const emptyRow = document.createElement('tr');
        emptyRow.innerHTML = `<td colspan="5" class="no-meetings">No active meetings</td>`;
        tableBody.appendChild(emptyRow);
    } else {
        // Add rows for each meeting
        meetings.forEach(function(meeting) {
            const row = document.createElement('tr');
            
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
                <td>${meeting.meeting.topic || 'Unnamed Meeting'}</td>
                <td class="${statusClass}">${statusText}</td>
                <td>${meeting.participantCount}</td>
                <td>${formatTime(meeting.startedAt)}</td>
                <td>${meeting.meeting.roomName || 'No Room'}</td>
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