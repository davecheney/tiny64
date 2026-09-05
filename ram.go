package tiny64

var ram [65536]byte

func Ram() []byte {
	return ram[:]
}
