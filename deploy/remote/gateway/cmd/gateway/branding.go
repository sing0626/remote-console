package main

import (
	"io"
	"net/http"
	"remoteconsole.local/connect/releaseinfo"
)

func serveBrandAsset(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/_remoteconsole/brand.svg" {
		return false
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, releaseinfo.IconSVG)
	return true
}
