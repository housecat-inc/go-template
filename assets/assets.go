package assets

//go:generate tailwindcss -i css/input.css -o css/output.css --minify

import "embed"

// FS contains the built static assets (css/output.css, js/*.js).
// input.css is excluded — it is a build-time source file only.
//
//go:embed css/output.css js/*
var FS embed.FS
