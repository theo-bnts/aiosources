package model

import (
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var byteInteger = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:[ ,.\x{00a0}\x{202f}][0-9]{3})+)$`)
var byteSize = regexp.MustCompile(`(?i)^([0-9]+(?:[.,][0-9]+)?)\s*(B|O|bytes|octets|[KMGTPE](?:i?[BO])?)$`)

// ParseByteSize accepts exact byte counts or displayed decimal/binary sizes.
func ParseByteSize(value string) (int64, error) {
	// Keep exact decimal arithmetic bounded even for an invalid upstream field.
	if len(value) > 256 {
		return 0, errors.New("Size exceeds the supported range")
	}
	value = strings.Join(strings.Fields(value), " ")
	if byteInteger.MatchString(value) {
		plain := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == ',' || r == '.' {
				return -1
			}
			return r
		}, value)
		n, err := strconv.ParseInt(plain, 10, 64)
		if err != nil || n < 0 {
			return 0, errors.New("Size exceeds the supported range")
		}
		return n, nil
	}
	match := byteSize.FindStringSubmatch(value)
	if len(match) == 0 {
		return 0, errors.New("Size must be bytes or a value such as 1.5 GiB")
	}
	amount, ok := new(big.Rat).SetString(strings.ReplaceAll(match[1], ",", "."))
	if !ok {
		return 0, errors.New("Invalid size")
	}
	unit := strings.ToUpper(match[2])
	if unit == "BYTES" || unit == "OCTETS" {
		unit = "B"
	}
	unit = strings.ReplaceAll(unit, "O", "B")
	if unit != "B" {
		power := strings.IndexByte("KMGTPE", unit[0]) + 1
		base := int64(1000)
		if strings.Contains(unit, "I") || len(unit) == 1 {
			base = 1024
		}
		factor := new(big.Int).Exp(big.NewInt(base), big.NewInt(int64(power)), nil)
		amount.Mul(amount, new(big.Rat).SetInt(factor))
	}
	// Displayed decimal sizes are approximate. Round once, without float64
	// precision loss, and reject values outside PostgreSQL's signed range.
	rounded, ok := new(big.Int).SetString(amount.FloatString(0), 10)
	if !ok || !rounded.IsInt64() || rounded.Sign() < 0 {
		return 0, errors.New("Size exceeds the supported range")
	}
	return rounded.Int64(), nil
}
