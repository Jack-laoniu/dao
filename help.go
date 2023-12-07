package orm

func lowUpperString(s string) string {
	b := make([]byte, 0)
	last := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
			if i != 0 && last != i-1 {
				b = append(b, '_')
			}
			last = i
		}
		b = append(b, c)
	}
	return string(b)
}
