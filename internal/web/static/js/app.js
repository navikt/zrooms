// Simplified app.js for Zrooms with HTMX support
document.addEventListener('DOMContentLoaded', function() {
    // We don't need much JavaScript since HTMX handles most of the dynamic behavior
    console.log('Zrooms application loaded with HTMX support');
    
    // Only apply these enhancements if SSE is not enabled or HTMX is not supported
    if (!useSSE || !htmx) {
        setupRefreshCounter();
    }
    
    // Apply alternating row colors
    applyRowStyles();
    
    // Set up meeting count in page title
    updateMeetingCountInTitle();
    
    // Update the "last updated" timestamp when HTMX completes a swap
    document.body.addEventListener('htmx:afterSwap', function(event) {
        const now = new Date();
        const timeString = now.toLocaleTimeString();
        const lastUpdatedElem = document.getElementById('last-updated');
        if (lastUpdatedElem) {
            lastUpdatedElem.textContent = timeString;
        }
        
        // Re-apply styles after content updates
        applyRowStyles();
        updateMeetingCountInTitle();
    });
    
    // Add status indicator for SSE connection
    if (useSSE && htmx) {
        document.body.addEventListener('htmx:sseOpen', function() {
            console.log('SSE connection opened');
            addConnectionIndicator('connected');
        });
        
        document.body.addEventListener('htmx:sseError', function() {
            console.log('SSE connection error');
            addConnectionIndicator('error');
        });
    }
});

// Add a visual indicator for SSE connection status
function addConnectionIndicator(status) {
    let indicator = document.getElementById('sse-indicator');
    if (!indicator) {
        indicator = document.createElement('div');
        indicator.id = 'sse-indicator';
        indicator.style.position = 'fixed';
        indicator.style.bottom = '10px';
        indicator.style.right = '10px';
        indicator.style.padding = '5px 10px';
        indicator.style.borderRadius = '3px';
        indicator.style.fontSize = '12px';
        indicator.style.fontWeight = 'bold';
        document.body.appendChild(indicator);
    }
    
    if (status === 'connected') {
        indicator.textContent = '● Live Updates Active';
        indicator.style.backgroundColor = 'rgba(0, 128, 0, 0.7)';
        indicator.style.color = 'white';
    } else {
        indicator.textContent = '● Using Page Refresh';
        indicator.style.backgroundColor = 'rgba(255, 0, 0, 0.7)';
        indicator.style.color = 'white';
    }
}

// Apply alternating row styles
function applyRowStyles() {
    const rows = document.querySelectorAll('tbody tr');
    rows.forEach((row, i) => {
        if (i % 2 !== 0) {
            row.style.backgroundColor = 'rgba(0, 0, 0, 0.02)';
        } else {
            row.style.backgroundColor = '';
        }
    });
}

// Updates the meeting count in the browser title
function updateMeetingCountInTitle() {
    const meetingRows = document.querySelectorAll('tbody tr');
    const noMeetingsRow = document.querySelector('.no-meetings');
    
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