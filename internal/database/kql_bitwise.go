package database

import "math/bits"

func kqlBinaryAnd(left, right any) any {
	a, ok := left.(int64)
	b, valid := right.(int64)
	if !ok || !valid {
		return nil
	}
	return a & b
}
func kqlBinaryOr(left, right any) any {
	a, ok := left.(int64)
	b, valid := right.(int64)
	if !ok || !valid {
		return nil
	}
	return a | b
}
func kqlBinaryXor(left, right any) any {
	a, ok := left.(int64)
	b, valid := right.(int64)
	if !ok || !valid {
		return nil
	}
	return a ^ b
}
func kqlBinaryNot(value any) any {
	number, ok := value.(int64)
	if !ok {
		return nil
	}
	return ^number
}
func kqlBinaryShiftLeft(value, shift any) any {
	number, ok := value.(int64)
	count, valid := shift.(int64)
	if !ok || !valid || count < 0 {
		return nil
	}
	return number << uint(count%64)
}
func kqlBinaryShiftRight(value, shift any) any {
	number, ok := value.(int64)
	count, valid := shift.(int64)
	if !ok || !valid || count < 0 {
		return nil
	}
	return number >> uint(count%64)
}
func kqlBitsetCountOnes(value any) any {
	number, ok := value.(int64)
	if !ok {
		return nil
	}
	return int64(bits.OnesCount64(uint64(number)))
}
