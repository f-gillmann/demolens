package geom

import (
	"math"
	"sort"

	"github.com/golang/geo/r3"
)

// fmin/fmax reproduce math.Min/math.Max exactly but skip the archMin/archMax
// call on the common path (distinct finite operands), which the CPU profile
// showed was ~40% of the whole program. For strict inequality the comparison IS
// the answer; only ties (incl. signed zero) and NaN — where the builtin min/max
// disagree with math on the sign/NaN/Inf corners — fall through to math, so the
// result stays bit-identical to the original math.Min/Max slab test.
func fmin(a, b float64) float64 {
	if a < b {
		return a
	}
	if a > b {
		return b
	}
	return math.Min(a, b)
}

func fmax(a, b float64) float64 {
	if a > b {
		return a
	}
	if a < b {
		return b
	}
	return math.Max(a, b)
}

// most triangles we let a leaf hold before splitting.
const bvhLeafSize = 8

// one BVH node. leaves point at a [start,end) range in Mesh.order; interior
// nodes point at their two children.
type bvhNode struct {
	min, max    r3.Vector
	left, right int // child indices, -1 means leaf
	start, end  int // triangle range, leaf only
}

// median-split BVH so Occluded is ~O(log n) instead of testing every triangle.
func (m *Mesh) buildBVH() {
	n := len(m.tris)
	if n == 0 {
		return
	}
	m.order = make([]int, n)
	cent := make([]r3.Vector, n)
	lo := make([]r3.Vector, n)
	hi := make([]r3.Vector, n)
	for i, t := range m.tris {
		m.order[i] = i
		a, b := triBounds(t)
		lo[i], hi[i] = a, b
		cent[i] = a.Add(b).Mul(0.5)
	}
	m.nodes = make([]bvhNode, 0, 2*n)

	var build func(start, end int) int
	build = func(start, end int) int {
		nmin, nmax := lo[m.order[start]], hi[m.order[start]]
		for i := start + 1; i < end; i++ {
			t := m.order[i]
			nmin, nmax = minVec(nmin, lo[t]), maxVec(nmax, hi[t])
		}

		idx := len(m.nodes)
		m.nodes = append(m.nodes, bvhNode{min: nmin, max: nmax, left: -1, right: -1, start: start, end: end})
		if end-start <= bvhLeafSize {
			return idx
		}

		ext := nmax.Sub(nmin)
		axis := 0
		if ext.Y >= ext.X && ext.Y >= ext.Z {
			axis = 1
		} else if ext.Z >= ext.X && ext.Z >= ext.Y {
			axis = 2
		}

		sub := m.order[start:end]
		sort.Slice(sub, func(a, b int) bool { return axisVal(cent[sub[a]], axis) < axisVal(cent[sub[b]], axis) })
		mid := (start + end) / 2
		l := build(start, mid)
		r := build(mid, end)
		m.nodes[idx].left, m.nodes[idx].right = l, r
		return idx
	}

	build(0, n)
}

// Occluded walks the BVH and returns true on the first triangle that blocks the
// segment from..to. bails early, doesn't care which triangle.
func (m *Mesh) Occluded(from, to r3.Vector) bool {
	if len(m.nodes) == 0 {
		return false
	}

	dir := to.Sub(from)
	inv := r3.Vector{X: 1 / dir.X, Y: 1 / dir.Y, Z: 1 / dir.Z}
	stack := [64]int{}
	sp := 0
	stack[sp] = 0
	sp++

	for sp > 0 {
		sp--
		nd := &m.nodes[stack[sp]] // pointer: avoid copying the ~80B node each pop
		if !slabHit(from, inv, nd.min, nd.max) {
			continue
		}

		if nd.left < 0 {
			for i := nd.start; i < nd.end; i++ {
				if m.tris[m.order[i]].blocks(from, dir) {
					return true
				}
			}
			continue
		}

		if sp+2 <= len(stack) {
			stack[sp] = nd.left
			stack[sp+1] = nd.right
			sp += 2
		}
	}

	return false
}

// slab test of ray (orig, 1/dir) against an AABB, clamped to t in [0,1].
// Uses the builtin min/max: same IEEE NaN/-0 semantics as math.Min/Max (so
// bit-identical results) but inlined instead of an archMin/archMax call, which
// the CPU profile showed was ~40% of the whole program's time.
func slabHit(orig, inv, lo, hi r3.Vector) bool {
	t1 := (lo.X - orig.X) * inv.X
	t2 := (hi.X - orig.X) * inv.X
	tmin := fmin(t1, t2)
	tmax := fmax(t1, t2)
	t1, t2 = (lo.Y-orig.Y)*inv.Y, (hi.Y-orig.Y)*inv.Y
	tmin = fmax(tmin, fmin(t1, t2))
	tmax = fmin(tmax, fmax(t1, t2))
	t1, t2 = (lo.Z-orig.Z)*inv.Z, (hi.Z-orig.Z)*inv.Z
	tmin = fmax(tmin, fmin(t1, t2))
	tmax = fmin(tmax, fmax(t1, t2))
	return tmax >= fmax(tmin, 0) && tmin <= 1
}

func triBounds(t triangle) (lo, hi r3.Vector) {
	lo = minVec(minVec(t.a, t.b), t.c)
	hi = maxVec(maxVec(t.a, t.b), t.c)
	return
}

func minVec(a, b r3.Vector) r3.Vector {
	return r3.Vector{X: fmin(a.X, b.X), Y: fmin(a.Y, b.Y), Z: fmin(a.Z, b.Z)}
}

func maxVec(a, b r3.Vector) r3.Vector {
	return r3.Vector{X: fmax(a.X, b.X), Y: fmax(a.Y, b.Y), Z: fmax(a.Z, b.Z)}
}

func axisVal(v r3.Vector, axis int) float64 {
	switch axis {
	case 1:
		return v.Y
	case 2:
		return v.Z
	}
	return v.X
}
