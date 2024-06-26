package randx

import (
	"crypto/rand"
	"math/big"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// String generates a random string with the given length, a-z, A-Z.
func String(n int) string {
	seq := make([]byte, n)
	max := big.NewInt(int64(len(letterBytes)))
	for i := range seq {
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		seq[i] = letterBytes[r.Int64()]
	}
	return string(seq)
}
