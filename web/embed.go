package web

import "embed"

//go:embed *.html *.css *.js *.ico
var Assets embed.FS
