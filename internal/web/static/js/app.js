// Simplified app.js for Zrooms with HTMX support
document.addEventListener('DOMContentLoaded', function() {
    // We don't need much JavaScript since HTMX handles most of the dynamic behavior
    console.log('Zrooms application loaded with HTMX support');
    
    // Apply alternating row colors
    applyRowStyles();
    
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
    });
    
    // Add status indicator for SSE connection (always enabled)
    document.body.addEventListener('htmx:sseOpen', function(event) {
        console.log('SSE connection opened', event.detail);
        addConnectionIndicator('connected');
    });
    
    document.body.addEventListener('htmx:sseError', function(event) {
        console.log('SSE connection error', event.detail);
        addConnectionIndicator('error');
    });
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