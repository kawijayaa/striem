package database

import (
	"fmt"
	"math"
)

// Moments are accumulated relative to the first input so integer values with
// a large common offset do not lose their differences when cast to float64.
type statistic struct {
	count            int64
	origin           any
	mean, m2         float64
	population, root bool
}

func shiftedNumber(value, origin any) (float64, bool) {
	if v, ok := value.(int64); ok {
		if base, ok := origin.(int64); ok {
			if v >= base {
				return float64(uint64(v) - uint64(base)), true
			}
			return -float64(uint64(base) - uint64(v)), true
		}
	}
	v, ok := number(value)
	if !ok {
		return 0, false
	}
	base, ok := number(origin)
	return v - base, ok
}

func (s *statistic) Step(value any) error {
	if v, ok := value.(float64); ok && (math.IsNaN(v) || math.IsInf(v, 0)) {
		return fmt.Errorf("statistical aggregate requires finite values")
	}
	if _, ok := number(value); !ok {
		return nil
	}
	if s.count == 0 {
		s.origin = value
	}
	x, _ := shiftedNumber(value, s.origin)
	s.count++
	delta := x - s.mean
	s.mean += delta / float64(s.count)
	s.m2 += delta * (x - s.mean)
	if math.IsNaN(s.m2) || math.IsInf(s.m2, 0) {
		return fmt.Errorf("statistical aggregate overflow")
	}
	return nil
}

func (s *statistic) Done() any {
	denominator := s.count
	if !s.population {
		denominator--
	}
	if denominator <= 0 {
		return nil
	}
	value := s.m2 / float64(denominator)
	if s.root {
		value = math.Sqrt(value)
	}
	return value
}

// covariance accumulates the co-moment of (x, y) pairs with Welford's
// update; pairs with a null or non-numeric member are skipped.
type covariance struct {
	count        int64
	meanX, meanY float64
	comoment     float64
	population   bool
}

func (c *covariance) Step(xValue, yValue any) error {
	for _, value := range []any{xValue, yValue} {
		if v, ok := value.(float64); ok && (math.IsNaN(v) || math.IsInf(v, 0)) {
			return fmt.Errorf("statistical aggregate requires finite values")
		}
	}
	x, okX := number(xValue)
	y, okY := number(yValue)
	if !okX || !okY {
		return nil
	}
	c.count++
	deltaX := x - c.meanX
	c.meanX += deltaX / float64(c.count)
	c.meanY += (y - c.meanY) / float64(c.count)
	c.comoment += deltaX * (y - c.meanY)
	if math.IsNaN(c.comoment) || math.IsInf(c.comoment, 0) {
		return fmt.Errorf("statistical aggregate overflow")
	}
	return nil
}

func (c *covariance) Done() any {
	denominator := c.count
	if !c.population {
		denominator--
	}
	if denominator <= 0 {
		return nil
	}
	return c.comoment / float64(denominator)
}
