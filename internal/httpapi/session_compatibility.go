package httpapi

import (
	"net/http"
	"time"
)

// Existing sessions remain valid during the application-name transition. A
// present new cookie always wins, including when invalid: it cannot fall back
// to a different identity hidden in an older cookie.
const previousSessionCookie = "ingest_session"

func requestSessionCookie(r *http.Request) (*http.Cookie, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err == http.ErrNoCookie {
		return r.Cookie(previousSessionCookie)
	}
	return cookie, err
}

func (a *authentication) retirePreviousSessionCookie(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(previousSessionCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: previousSessionCookie, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1})
	}
}
