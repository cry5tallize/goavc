package decoder

func (c *cabacSyntax) bType(fs *frameState, addr int) int {
	inc := 0
	for side, other := range []int{addr - 1, addr - fs.mbW} {
		if side == 0 && addr%fs.mbW == 0 {
			continue
		}
		if fs.neighborAvailable(addr, other) && !fs.info[other].direct {
			inc++
		}
	}
	if c.bin(27+inc) == 0 {
		return 0
	}
	if c.bin(30) == 0 {
		return 1 + c.bin(32)
	}
	v := 8*c.bin(31) + 4*c.bin(32) + 2*c.bin(32) + c.bin(32)
	switch {
	case v < 8:
		return v + 3
	case v == 13:
		return 23 + c.intraType(fs, addr, 32, false)
	case v == 14:
		return 11
	case v == 15:
		return 22
	default:
		return 2*v + c.bin(32) - 4
	}
}

func (s *sliceEntry) readBSubType() (int, error) {
	if s.cabac == nil {
		v, err := s.data.ReadUE()
		return int(v), err
	}
	c := s.cabac
	if c.bin(36) == 0 {
		return 0, c.d.Err()
	}
	if c.bin(37) == 0 {
		return 1 + c.bin(39), c.d.Err()
	}
	v := 3
	if c.bin(38) != 0 {
		if c.bin(39) != 0 {
			return 11 + c.bin(39), c.d.Err()
		}
		v += 4
	}
	v += 2*c.bin(39) + c.bin(39)
	return v, c.d.Err()
}
