package httpapi

import (
	"net/http"
	"time"

	"github.com/theo-bnts/aiosources/internal/store"
)

// Existing sessions remain valid during the application-name transition. A
// present new cookie always wins, including when invalid: it cannot fall back
// to a different identity hidden in an older cookie.
const previousSessionCookie = "ingest_session"

func singleCookie(r *http.Request, name string) (*http.Cookie, error) {
	var found *http.Cookie
	for _, candidate := range r.Cookies() {
		if candidate.Name == name {
			if found != nil {
				return nil, store.ErrAuthentication
			}
			found = candidate
		}
	}
	if found == nil {
		return nil, http.ErrNoCookie
	}
	return found, nil
}

func requestSessionCookie(r *http.Request) (*http.Cookie, error) {
	cookie, err := singleCookie(r, sessionCookie)
	if err == http.ErrNoCookie {
		return singleCookie(r, previousSessionCookie)
	}
	return cookie, err
}

func (a *authentication) retirePreviousSessionCookie(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(previousSessionCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: previousSessionCookie, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1})
	}
}
