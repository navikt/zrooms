package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/config"
)

const oauthCookieName = "zrooms_zoom_oauth"

// OAuthAuthorizer persists an authorization only after exchanging and identifying it.
type OAuthAuthorizer interface {
	Authorize(context.Context, string, time.Time) error
}

// OAuthHandler binds the public app's authorization flow to the initiating browser.
type OAuthHandler struct {
	config     config.ZoomConfig
	authorizer OAuthAuthorizer
}

// NewOAuthHandler uses the existing Zoom client credentials.
func NewOAuthHandler(cfg config.ZoomConfig, authorizer OAuthAuthorizer) *OAuthHandler {
	return &OAuthHandler{config: cfg, authorizer: authorizer}
}

func (h *OAuthHandler) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: oauthCookieName, Value: value, Path: "/oauth",
		Secure:   strings.HasPrefix(h.config.RedirectURI, "https://"),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
}

func (h *OAuthHandler) sign(value string) string {
	mac := hmac.New(sha256.New, []byte(h.config.ClientSecret))
	_, _ = io.WriteString(mac, "zrooms/oauth-state/v1:"+value)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *OAuthHandler) prepare(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !h.config.IsZoomConfigValid() {
		http.Error(w, "Zoom OAuth is not configured", http.StatusServiceUnavailable)
		return false
	}
	redirect, err := url.Parse(h.config.RedirectURI)
	if err != nil || redirect.Host == "" || redirect.User != nil ||
		(redirect.Scheme != "https" && !(redirect.Scheme == "http" && (redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1"))) {
		log.Print("Zoom OAuth requires an HTTPS redirect URI outside localhost")
		http.Error(w, "Zoom OAuth configuration is invalid", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// Start creates a short-lived, signed state cookie before redirecting to Zoom.
func (h *OAuthHandler) Start(w http.ResponseWriter, r *http.Request) {
	if !h.prepare(w, r) {
		return
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		log.Print("Unable to generate Zoom OAuth state")
		http.Error(w, "Unable to start Zoom authorization", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	value := state + "." + strconv.FormatInt(time.Now().UnixMilli(), 10)
	http.SetCookie(w, h.cookie(value+"."+h.sign(value), 600))
	target, err := url.Parse(h.config.GetOAuthURL())
	if err != nil {
		http.Error(w, "Unable to start Zoom authorization", http.StatusServiceUnavailable)
		return
	}
	query := target.Query()
	query.Set("state", state)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

// Redirect refuses unsolicited callbacks, then exchanges the code before reporting success.
func (h *OAuthHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	if !h.prepare(w, r) {
		return
	}
	cookie, err := r.Cookie(oauthCookieName)
	http.SetCookie(w, h.cookie("", -1))
	if err != nil {
		h.invalidState(w)
		return
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || len(parts[0]) != 43 ||
		!hmac.Equal([]byte(h.sign(parts[0]+"."+parts[1])), []byte(parts[2])) ||
		!hmac.Equal([]byte(r.URL.Query().Get("state")), []byte(parts[0])) {
		h.invalidState(w)
		return
	}
	issued, err := strconv.ParseInt(parts[1], 10, 64)
	started := time.UnixMilli(issued)
	if err != nil || started.After(time.Now()) || time.Since(started) > 10*time.Minute {
		h.invalidState(w)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Error(w, "Zoom authorization was declined. No authorization was stored.", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		http.Error(w, "Missing or invalid authorization code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.authorizer.Authorize(ctx, code, started); err != nil {
		log.Printf("Zoom OAuth authorization failed: %v", err)
		http.Error(w, "Unable to complete Zoom authorization. Please start again using Add Your Meetings.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>Zoom connected</title></head><body><h1>Authorization successful</h1><p>Your Zoom authorization has been stored. You can close this window.</p><a href="/">Return to Zrooms</a></body></html>`)
}

func (h *OAuthHandler) invalidState(w http.ResponseWriter) {
	log.Print("Zoom OAuth callback rejected: invalid or missing state")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>Restart Zoom authorization</title></head><body><h1>Authorization could not be verified</h1><p>The request expired or was not started in this browser.</p><a href="/oauth/start">Start Zoom authorization again</a></body></html>`)
}
