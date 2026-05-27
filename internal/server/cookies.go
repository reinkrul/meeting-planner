package server

import (
	"net/http"
	"net/url"
)

const (
	cookieUserName  = "mp_user_name"
	cookieUserEmail = "mp_user_email"
	// 90 days. Plenty for "remember me on this device" without being
	// effectively permanent.
	cookieMaxAge = 60 * 60 * 24 * 90
)

// setUserCookies remembers the booker's name and email so the form pre-fills
// on return visits from the same browser. HttpOnly: the values are only
// needed server-side for pre-fill; JS doesn't need them.
func setUserCookies(w http.ResponseWriter, name, email string) {
	common := http.Cookie{
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	nameCookie := common
	nameCookie.Name = cookieUserName
	nameCookie.Value = url.QueryEscape(name)
	http.SetCookie(w, &nameCookie)

	emailCookie := common
	emailCookie.Name = cookieUserEmail
	emailCookie.Value = url.QueryEscape(email)
	http.SetCookie(w, &emailCookie)
}

// readUserCookies returns the remembered name and email, or empty strings
// if the cookies are absent or malformed.
func readUserCookies(r *http.Request) (name, email string) {
	if c, err := r.Cookie(cookieUserName); err == nil {
		if v, err := url.QueryUnescape(c.Value); err == nil {
			name = v
		}
	}
	if c, err := r.Cookie(cookieUserEmail); err == nil {
		if v, err := url.QueryUnescape(c.Value); err == nil {
			email = v
		}
	}
	return
}
