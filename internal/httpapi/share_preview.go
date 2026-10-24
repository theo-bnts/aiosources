package httpapi

import (
	"html"
	"net/url"
)

func sharePreviewOrigin(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return ""
	}
	return html.EscapeString(u.Scheme + "://" + u.Host)
}
