package reservation

const (
	maxNameLen     = 64
	maxSingleValue = 1 << 20
	maxScanLimit   = 1000
)

func validName(s string) bool {
	n := len(s)
	if n < 1 || n > maxNameLen {
		return false
	}
	for i := 0; i < n; i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
