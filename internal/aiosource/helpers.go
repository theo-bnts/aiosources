package aiosource

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
)

func itoa64(n int64) string       { return strconv.FormatInt(n, 10) }
func strconvPair(a, b int) string { return strconv.Itoa(a) + ":" + strconv.Itoa(b) }
func fingerprint(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

var hashRE = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

var regexpIMDb = regexp.MustCompile(`^tt[0-9]+$`)
