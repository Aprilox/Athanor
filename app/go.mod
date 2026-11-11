module athanor

go 1.27.4

require (
	github.com/ebitengine/purego v0.8.4
	github.com/jchv/go-webview2 v0.0.0
	golang.org/x/crypto v0.58.0
	golang.org/x/sys v0.50.0
	rsc.io/qr v0.2.0
)

require github.com/jchv/go-winloader v0.0.0 // indirect

replace github.com/jchv/go-webview2 => ./third_party/go-webview2

replace github.com/jchv/go-winloader => ./third_party/go-winloader

replace github.com/ebitengine/purego => ./third_party/purego

replace rsc.io/qr => ./third_party/qr
