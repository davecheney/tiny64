package desktop

import (
	"testing"

	"github.com/davecheney/tiny64"
)

// The two backends declare their own positional and shifted tables over
// their own host key type, and the build tags make them mutually
// exclusive, so no test can compare them directly. These check whichever
// pair was built against the matrix instead, which catches the two ways
// the tables can be wrong without either backend having to know about the
// other.

// TestKeymapCoversMatrix checks every key in the C64 matrix is reachable.
func TestKeymapCoversMatrix(t *testing.T) {
	mapped := make(map[tiny64.Key]bool)
	for _, c64 := range positional {
		mapped[c64] = true
	}
	for _, c64 := range shifted {
		mapped[c64] = true
	}

	for key := range tiny64.Key(64) {
		// KeyEquals is the one position with nowhere to go. The C64's
		// top row is two keys wider than a PC's, so by the time the
		// map works right along it there is no host key left to put
		// '=' on. Anything else missing is a gap in the table.
		if key == tiny64.KeyEquals {
			if mapped[key] {
				t.Errorf("KeyEquals is mapped; it has no positional home, so the comment in keyboard.go needs updating")
			}
			continue
		}
		if !mapped[key] {
			t.Errorf("matrix key at PA%d/PB%d is unreachable", key.Row(), key.Col())
		}
	}
}

// TestNoHostKeyInBothTables checks no host key appears in both tables,
// which would press its C64 key and synthesize SHIFT at the same time.
func TestNoHostKeyInBothTables(t *testing.T) {
	for host := range shifted {
		if c64, ok := positional[host]; ok {
			t.Errorf("host key %v is in both tables, and would also press %v", host, c64)
		}
	}
}
