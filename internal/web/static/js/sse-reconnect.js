/**
 * SSE Reconnection Handling for HTMX
 * This script detects SSE connection failures in HTMX and provides automatic reconnection.
 */
(function() {
    // Configuration for reconnection attempts
    const config = {
        maxRetries: 5,         // Maximum number of automatic retries
        initialDelay: 1000,    // Initial retry delay in milliseconds
        maxDelay: 30000,       // Maximum delay between retries
        backoffFactor: 1.5,    // Factor by which to increase delay on each retry
        debug: true            // Enable debug logging
    };

    // State tracking
    let state = {
        retryCount: 0,
        sseConnected: false,
        baseUrl: window.location.origin,
        eventSourceUrl: "/events",
        lastEventId: null,
        manualEventSource: null
    };

    // Logging function
    function log(message, ...args) {
        if (config.debug) {
            console.log(`[SSE Reconnect] ${message}`, ...args);
        }
    }

    // Function to create a manual EventSource connection
    function createManualEventSource() {
        try {
            if (state.manualEventSource) {
                state.manualEventSource.close();
            }
            
            // Use the full URL for connecting
            const url = new URL(state.eventSourceUrl, state.baseUrl);
            log("Attempting manual EventSource connection to", url.toString());

            // Initialize EventSource with withCredentials option to send cookies
            const es = new EventSource(url.toString(), { withCredentials: true });
            
            es.onopen = function() {
                log("Manual EventSource connected successfully");
                state.sseConnected = true;
                state.retryCount = 0;
                
                // Forward events to HTMX
                document.body.dispatchEvent(new Event('htmx:sseOpen'));
            };
            
            es.onerror = function(err) {
                log("Manual EventSource error", err);
                state.sseConnected = false;
                document.body.dispatchEvent(new CustomEvent('htmx:sseError', { detail: { error: err }}));
                
                // Attempt to reconnect with backoff
                reconnectWithBackoff();
            };
            
            // Forward messages as HTMX SSE events
            es.addEventListener('update', function(e) {
                state.lastEventId = e.lastEventId;
                document.body.dispatchEvent(new CustomEvent('htmx:sseMessage', { detail: { elt: document.body }}));
                document.body.dispatchEvent(new CustomEvent('sse:update', { detail: { elt: document.body }}));
            });
            
            es.addEventListener('initial-load', function(e) {
                state.lastEventId = e.lastEventId;
                document.body.dispatchEvent(new CustomEvent('sse:initial-load', { detail: { elt: document.body }}));
            });
            
            state.manualEventSource = es;
        } catch(err) {
            log("Error creating EventSource:", err);
            state.sseConnected = false;
        }
    }

    // Reconnect with exponential backoff
    function reconnectWithBackoff() {
        if (state.retryCount >= config.maxRetries) {
            log("Maximum retry count reached, stopping automatic reconnection");
            return;
        }
        
        // Calculate delay with exponential backoff
        const delay = Math.min(
            config.initialDelay * Math.pow(config.backoffFactor, state.retryCount),
            config.maxDelay
        );
        
        state.retryCount++;
        
        log(`Reconnecting in ${delay}ms (attempt ${state.retryCount}/${config.maxRetries})`);
        
        setTimeout(() => {
            if (!state.sseConnected) {
                createManualEventSource();
            }
        }, delay);
    }

    // Initialize when DOM is loaded
    document.addEventListener('DOMContentLoaded', function() {
        log("SSE reconnect handler initialized");
        
        // Listen for HTMX SSE errors
        document.body.addEventListener('htmx:sseError', function(event) {
            log("HTMX SSE error detected", event);
            
            if (!state.sseConnected && state.retryCount === 0) {
                // First error - set the URL from the element with SSE connection
                const sseElement = document.querySelector('[sse-connect]');
                if (sseElement) {
                    const sseUrl = sseElement.getAttribute('sse-connect');
                    if (sseUrl) {
                        state.eventSourceUrl = sseUrl;
                        log("Using SSE URL from element:", state.eventSourceUrl);
                    }
                }
                
                // Start reconnection process
                reconnectWithBackoff();
            }
        });
    });
})();
