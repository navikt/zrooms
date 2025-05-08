// app.js - Client-side functionality for ZRooms

document.addEventListener('DOMContentLoaded', function() {
    // Set up auto-refresh counter
    setupRefreshCounter();
    
    // Apply alternating row colors
    const rows = document.querySelectorAll('tbody tr');
    rows.forEach((row, i) => {
        if (i % 2 !== 0) {
            row.style.backgroundColor = 'rgba(0, 0, 0, 0.02)';
        }
    });
    
    // Set up meeting count in page title
    updateMeetingCountInTitle();
});

// Updates the meeting count in the browser title
function updateMeetingCountInTitle() {
    const meetingRows = document.querySelectorAll('tbody tr');
    const count = meetingRows.length;
    if (count > 0) {
        document.title = `(${count}) ZRooms - Meeting Status`;
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