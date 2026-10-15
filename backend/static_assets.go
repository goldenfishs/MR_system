package main

import (
	"path"
	"regexp"
	"strings"
)

// Only a single Vite content-hashed asset filename may be read from retained
// releases. HTML, nested paths and arbitrary data-volume files are never served.
var retainedAssetPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+-[A-Za-z0-9_-]{8,64}\.(js|mjs|css|woff|woff2|ttf|otf|eot|png|jpe?g|gif|webp|avif|svg|ico|wasm|mp4|webm)$`)

func retainedAssetName(urlPath string) string {
	if !strings.HasPrefix(urlPath, "/assets/") {
		return ""
	}
	name := strings.TrimPrefix(urlPath, "/assets/")
	if path.Base(name) != name || strings.ContainsAny(name, `/\`) || !retainedAssetPattern.MatchString(name) {
		return ""
	}
	return name
}
