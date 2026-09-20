module github.com/davecheney/tiny64

go 1.27.1

require (
	github.com/tinygo-org/pio v0.3.1-0.20260910075224-6982692d33a1
	tinygo.org/x/drivers v0.36.0
)

require github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect

replace github.com/tinygo-org/pio => github.com/davecheney/pio v0.0.0-20260911022417-d378a39501eb
