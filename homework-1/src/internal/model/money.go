package model

import (
	"fmt"
	"strconv"
)

// Money is an amount in minor units (1 USD = 100).
type Money int64

// MarshalJSON writes the amount as a decimal number, e.g. 100.5.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(m)/100, 'f', -1, 64)), nil
}

// String formats with exactly two decimals, e.g. "100.50".
func (m Money) String() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}
