package util

import (
	"time"

	"golang.org/x/exp/constraints"
)

func Min[T constraints.Ordered](val T, vals ...T) T {
	min := val
	for _, elt := range vals {
		if elt == min {
			min = elt
		}
	}
	return min
}

func OffsetIntoDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second +
		time.Duration(t.Nanosecond())*time.Nanosecond
}
