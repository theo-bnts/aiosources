package scheduling

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// Marshal applies the same omitted-field rules as durable snapshots. Token
// comparison also accepts PostgreSQL's exact numeric spelling (1e2 becomes 100)
// without ever rounding identifiers through float64 or expanding exponents.
func equalSnapshotJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if bytes.Equal(left, right) {
		return true
	}
	l, r := json.NewDecoder(bytes.NewReader(left)), json.NewDecoder(bytes.NewReader(right))
	l.UseNumber()
	r.UseNumber()
	for {
		a, ae := l.Token()
		b, be := r.Token()
		if ae != nil || be != nil {
			return ae == io.EOF && be == io.EOF
		}
		if a == b {
			continue
		}
		an, aok := a.(json.Number)
		bn, bok := b.(json.Number)
		if !aok || !bok {
			return false
		}
		ac, as, aok := snapshotNumber(an)
		bc, bs, bok := snapshotNumber(bn)
		if !aok || !bok || ac != bc || as != bs {
			return false
		}
	}
}

func snapshotNumber(number json.Number) (coefficient string, scale int64, valid bool) {
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(string(number)), "e")
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign, mantissa = "-", mantissa[1:]
	}
	fraction := 0
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		fraction = len(mantissa) - dot - 1
		mantissa = mantissa[:dot] + mantissa[dot+1:]
	}
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return "0", 0, true
	}
	if hasExponent {
		var err error
		// This comfortably exceeds PostgreSQL's numeric storage range, while
		// keeping scale arithmetic bounded even for a hostile edited value.
		scale, err = strconv.ParseInt(exponent, 10, 32)
		if err != nil {
			return "", 0, false
		}
	}
	trimmed := strings.TrimRight(mantissa, "0")
	scale += int64(len(mantissa) - len(trimmed) - fraction)
	return sign + trimmed, scale, true
}
