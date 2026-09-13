module github.com/davecheney/tiny64

go 1.27.1

require (
	github.com/hajimehoshi/ebiten/v2 v2.10.1
	github.com/tinygo-org/pio v0.3.1-0.20260910075224-6982692d33a1
	tinygo.org/x/drivers v0.36.0
)

require (
	github.com/ebitengine/gomobile v0.0.0-20260820040257-d11f821a26a6 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/tinygo-org/pio => github.com/davecheney/pio v0.0.0-20260911022417-d378a39501eb
