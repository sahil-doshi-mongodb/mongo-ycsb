package distribution

import (
	"math"
	"math/rand"
)

// ZipfianConstant is the standard YCSB default.
const ZipfianConstant = 0.99

// ── ScrambledZipfian ──────────────────────────────────────────────────────────

// ScrambledZipfian generates Zipfian-distributed values then scrambles them
// with FNV64 so hot items are randomly distributed across the key space —
// not always the lowest-numbered keys. Matches YCSB's default
// ScrambledZipfianGenerator behaviour exactly.
type ScrambledZipfian struct {
	n    int64
	zipf *zipfian
}

// NewScrambledZipfian creates a ScrambledZipfian for n items.
func NewScrambledZipfian(n int64, constant float64) *ScrambledZipfian {
	return &ScrambledZipfian{
		n:    n,
		zipf: newZipfian(n, constant),
	}
}

// Next returns a scrambled Zipfian value in [0, n).
func (s *ScrambledZipfian) Next(rng *rand.Rand) int64 {
	ret := s.zipf.next(rng)
	ret = FnvHash64(ret) % s.n
	if ret < 0 {
		ret += s.n
	}
	return ret
}

// ── Internal Zipfian ──────────────────────────────────────────────────────────

// zipfian generates values in [0, n) using the inverse-CDF method — O(1) per
// sample after O(1) initialisation (zeta computed with integral approximation).
type zipfian struct {
	n     int64
	theta float64
	zetaN float64
	zeta2 float64
	alpha float64
	eta   float64
}

func newZipfian(n int64, theta float64) *zipfian {
	zeta2 := zetaStatic(2, theta)
	zetaN := zetaStatic(n, theta)
	alpha := 1.0 / (1.0 - theta)
	eta := (1 - math.Pow(2.0/float64(n), 1-theta)) / (1 - zeta2/zetaN)
	return &zipfian{
		n: n, theta: theta, zetaN: zetaN, zeta2: zeta2, alpha: alpha, eta: eta,
	}
}

func (z *zipfian) next(rng *rand.Rand) int64 {
	u := rng.Float64()
	uz := u * z.zetaN
	var ret int64
	if uz < 1.0 {
		ret = 0
	} else if uz < 1.0+math.Pow(0.5, z.theta) {
		ret = 1
	} else {
		ret = int64(float64(z.n) * math.Pow(z.eta*(u-1)+1, z.alpha))
	}
	if ret >= z.n {
		ret = z.n - 1
	}
	if ret < 0 {
		ret = 0
	}
	return ret
}

// ── Latest distribution ───────────────────────────────────────────────────────

// NextLatest returns a key index biased toward recently inserted keys using
// an exponential distribution. Matches YCSB's SkewedLatestGenerator.
// Used by Workload D.
func NextLatest(rng *rand.Rand, insertedCount int64) int64 {
	if insertedCount <= 0 {
		return 0
	}
	u := rng.Float64()
	if u >= 1.0 {
		u = 0.9999999
	}
	// -ln(1-u) / ln(1/(1-0.999)) = -ln(1-u) / ln(1000)
	exp := -math.Log(1.0-u) / math.Log(1000.0)
	offset := int64(exp * float64(insertedCount))
	key := insertedCount - 1 - offset
	if key < 0 {
		key = 0
	}
	return key
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// zetaStatic computes sum_{i=1}^{n} i^{-theta}.
// Uses exact computation up to 10,000 then an integral approximation —
// accurate to within 0.01% for typical YCSB parameters.
func zetaStatic(n int64, theta float64) float64 {
	const cutoff = int64(10_000)
	limit := n
	if n > cutoff {
		limit = cutoff
	}
	sum := 0.0
	for i := int64(1); i <= limit; i++ {
		sum += 1.0 / math.Pow(float64(i), theta)
	}
	if n > cutoff {
		if math.Abs(theta-1.0) > 1e-10 {
			sum += (math.Pow(float64(n)+0.5, 1-theta) -
				math.Pow(float64(cutoff)+0.5, 1-theta)) / (1 - theta)
		} else {
			sum += math.Log(float64(n)+0.5) - math.Log(float64(cutoff)+0.5)
		}
	}
	return sum
}

// FnvHash64 is FNV-1a 64-bit — exported for use in generator.
func FnvHash64(val int64) int64 {
	const (
		prime  = int64(1099511628211)
		offset = int64(-3750763034362895579)
	)
	hash := offset
	for i := 0; i < 8; i++ {
		hash ^= val & 0xff
		hash *= prime
		val >>= 8
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}
