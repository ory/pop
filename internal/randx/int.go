package randx

import (
	"crypto/rand"
	"math"
	"math/big"
)

// NonNegativeInt returns a non-negative random int from a crypto-safe source.
func NonNegativeInt() int {
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt))
	if err != nil {
		panic(err)
	}
	return int(n.Int64())
}

// Int63 returns a a non-negative random int64 from a crypto-safe source.
func Int63() int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}
	return n.Int64()
}
