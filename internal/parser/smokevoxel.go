package parser

// CS2 servers compute the volumetric smoke fill and network it into GOTV as
// CSmokeGrenadeProjectile.m_VoxelFrameData: an append-only chunk stream that
// encodes exact per-tick voxel occupancy on a 32x32x32 grid anchored at the
// detonation position. Decoding it replaces the old 144u-sphere occlusion
// guess with the cloud's real shape (corridor flood, wall clipping, fade).
//
// Grid mapping, measured over 349 decoded smokes (open-air extents are
// exactly symmetric around 15.5 and span 24 voxels = the known 288u cloud
// width; the floor plane always lands inside voxel 15): voxel v spans
// [det+(v-16)*12, det+(v-15)*12] per axis, i.e. centers at det+(v-15.5)*12,
// grid min-corner at det-192. The grid is det-anchored, not world-snapped.
//
// Wire format, reverse-engineered from the same demo corpus: chunks
// of [u16 seq][u16 len][payload], payload [u8 phaseFlag][u8 type]. Type 3 is
// the detonation keyframe: [u8 n] + n 8-byte seeds [x][y][z][density][4B pad],
// then [u16 m] + m 10-byte mask entries. Type 2 is a delta of mask entries
// only, type 0 a heartbeat. A mask entry is [u16 cellIdx][u64 mask]: cellIdx
// is 3-bit-per-axis Morton over 8x8x8 cells, mask bits 2-bit-per-axis Morton
// over that cell's 4x4x4 voxels. Masks replace their cell wholesale. The
// masks encode mostly the cloud's shell, so the interior is filled here by
// flooding the complement from the grid boundary. phaseFlag flips nonzero at
// ~19s when the cloud starts dissolving and the stream ends at ~22s.

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"

	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

// edge length of one smoke voxel in game units
const smokeVoxelSize = 12.0

// voxelSmoke is the decoded volumetric state of one smoke projectile.
type voxelSmoke struct {
	det    r3.Vector // m_vSmokeDetonationPos, the grid anchor
	ready  bool      // a keyframe has been decoded, occ is usable
	fading bool      // phaseFlag flipped: cloud dissolving, stops blocking

	buf    []byte            // accumulated m_VoxelFrameData bytes
	parsed int               // bytes of buf consumed as complete chunks
	seeds  [][3]int          // keyframe seed voxels
	masks  map[uint16]uint64 // latest mask per 4x4x4 cell

	occ      [512]uint64 // 32^3 occupancy bitset, idx = x | y<<5 | z<<10
	min, max [3]int      // occupied voxel index bounds for the quick reject
}

// onSmokeVoxelTables wires the voxel-stream ingestion once net tables exist.
// Demos without the stream (pre-voxel CS2) simply never create the property,
// and their smokes keep the sphere fallback in smokeBlocked.
func (st *parseState) onSmokeVoxelTables(_ events.DataTablesParsed) {
	sc := st.parsed.ServerClasses().FindByName("CSmokeGrenadeProjectile")
	if sc == nil {
		return
	}
	sc.OnEntityCreated(func(ent sendtables.Entity) {
		upd := ent.Property("m_nVoxelUpdate")
		if upd == nil {
			return
		}
		vs := &voxelSmoke{masks: map[uint16]uint64{}}
		id := ent.ID()
		st.vision.voxelSmokes[id] = vs
		// m_nVoxelUpdate sits after the frame-data array and its size field in
		// the send table, so by the time it updates the tick's bytes are in.
		upd.OnUpdate(func(_ sendtables.PropertyValue) {
			st.ingestSmokeVoxels(ent, vs)
		})
		ent.OnDestroy(func() { delete(st.vision.voxelSmokes, id) })
	})
}

// voxelPropName returns "m_VoxelFrameData.NNNN", cached since the same names
// are looked up for every smoke in the demo.
var voxelPropNames []string

func voxelPropName(i int) string {
	for len(voxelPropNames) <= i {
		voxelPropNames = append(voxelPropNames, fmt.Sprintf("m_VoxelFrameData.%04d", len(voxelPropNames)))
	}
	return voxelPropNames[i]
}

// ingestSmokeVoxels appends the newly networked bytes to the smoke's chunk
// buffer and decodes any chunks they complete. Runs on the parse goroutine
// only, never concurrently with the LOS workers.
func (st *parseState) ingestSmokeVoxels(ent sendtables.Entity, vs *voxelSmoke) {
	szVal, ok := ent.PropertyValue("m_nVoxelFrameDataSize")
	if !ok {
		return
	}
	size, ok := szVal.Any.(int32)
	if !ok || int(size) <= len(vs.buf) {
		return
	}
	for i := len(vs.buf); i < int(size); i++ {
		v, ok := ent.PropertyValue(voxelPropName(i))
		if !ok {
			break
		}
		u, ok := v.Any.(uint64)
		if !ok {
			break
		}
		vs.buf = append(vs.buf, byte(u))
	}
	if vs.consumeChunks() {
		if det := ent.PropertyValueMust("m_vSmokeDetonationPos").R3VecOrNil(); det != nil {
			vs.det = *det
		}
		vs.rebuild()
	}
}

// consumeChunks parses complete chunks off the buffer and reports whether any
// of them changed the cloud shape.
func (vs *voxelSmoke) consumeChunks() bool {
	changed := false
	for vs.parsed+4 <= len(vs.buf) {
		ln := int(binary.LittleEndian.Uint16(vs.buf[vs.parsed+2:]))
		end := vs.parsed + 4 + ln
		if end > len(vs.buf) {
			break // chunk still streaming in
		}
		p := vs.buf[vs.parsed+4 : end]
		vs.parsed = end
		if len(p) < 2 {
			continue
		}
		if p[0] != 0 {
			vs.fading = true
		}
		switch p[1] {
		case 3: // detonation keyframe: fresh cloud state
			vs.seeds = vs.seeds[:0]
			clear(vs.masks)
			if len(p) < 3 {
				continue
			}
			n, o := int(p[2]), 3
			for i := 0; i < n && o+8 <= len(p); i++ {
				if p[o+3] > 0 {
					vs.seeds = append(vs.seeds, [3]int{int(p[o]), int(p[o+1]), int(p[o+2])})
				}
				o += 8
			}
			if o+2 <= len(p) {
				m := int(binary.LittleEndian.Uint16(p[o:]))
				o += 2
				for i := 0; i < m && o+10 <= len(p); i++ {
					vs.masks[binary.LittleEndian.Uint16(p[o:])] = binary.LittleEndian.Uint64(p[o+2:])
					o += 10
				}
			}
			changed = true
		case 2: // delta: replaced cell masks
			if len(p) < 4 {
				continue
			}
			m, o := int(binary.LittleEndian.Uint16(p[2:])), 4
			for i := 0; i < m && o+10 <= len(p); i++ {
				vs.masks[binary.LittleEndian.Uint16(p[o:])] = binary.LittleEndian.Uint64(p[o+2:])
				o += 10
			}
			changed = true
		}
	}
	return changed
}

// demorton splits an interleaved-per-axis Morton index back into x, y, z.
func demorton(i, bits int) (x, y, z int) {
	for b := 0; b < bits; b++ {
		x |= ((i >> (3 * b)) & 1) << b
		y |= ((i >> (3*b + 1)) & 1) << b
		z |= ((i >> (3*b + 2)) & 1) << b
	}
	return
}

// rebuild recomputes the occupancy bitset and bounds from seeds and masks.
func (vs *voxelSmoke) rebuild() {
	var occ [512]uint64
	set := func(x, y, z int) {
		if uint(x) < 32 && uint(y) < 32 && uint(z) < 32 {
			idx := x | y<<5 | z<<10
			occ[idx>>6] |= 1 << (idx & 63)
		}
	}
	for _, s := range vs.seeds {
		set(s[0], s[1], s[2])
	}
	for cell, mask := range vs.masks {
		cx, cy, cz := demorton(int(cell), 3)
		for b := 0; b < 64; b++ {
			if mask>>b&1 == 1 {
				sx, sy, sz := demorton(b, 2)
				set(cx*4+sx, cy*4+sy, cz*4+sz)
			}
		}
	}
	fillInterior(&occ)

	vs.min, vs.max = [3]int{31, 31, 31}, [3]int{0, 0, 0}
	any := false
	for w, word := range occ {
		for word != 0 {
			idx := w<<6 | bits.TrailingZeros64(word)
			word &= word - 1
			x, y, z := idx&31, idx>>5&31, idx>>10
			if x < vs.min[0] {
				vs.min[0] = x
			}
			if x > vs.max[0] {
				vs.max[0] = x
			}
			if y < vs.min[1] {
				vs.min[1] = y
			}
			if y > vs.max[1] {
				vs.max[1] = y
			}
			if z < vs.min[2] {
				vs.min[2] = z
			}
			if z > vs.max[2] {
				vs.max[2] = z
			}
			any = true
		}
	}
	vs.occ = occ
	vs.ready = any
}

// fillInterior marks everything not reachable from the grid boundary through
// empty cells as occupied: the stream networks mostly the cloud's shell, and
// a sightline between two players inside the cloud must still count blocked.
func fillInterior(occ *[512]uint64) {
	var outside [512]uint64
	stack := make([]int32, 0, 4096)
	push := func(idx int) {
		if occ[idx>>6]>>(idx&63)&1 == 0 && outside[idx>>6]>>(idx&63)&1 == 0 {
			outside[idx>>6] |= 1 << (idx & 63)
			stack = append(stack, int32(idx))
		}
	}
	for a := 0; a < 32; a++ {
		for b := 0; b < 32; b++ {
			push(a | b<<5)          // z = 0
			push(a | b<<5 | 31<<10) // z = 31
			push(a | b<<10)         // y = 0
			push(a | 31<<5 | b<<10) // y = 31
			push(a<<5 | b<<10)      // x = 0
			push(31 | a<<5 | b<<10) // x = 31
		}
	}
	for len(stack) > 0 {
		idx := int(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		x, y, z := idx&31, idx>>5&31, idx>>10
		if x > 0 {
			push(idx - 1)
		}
		if x < 31 {
			push(idx + 1)
		}
		if y > 0 {
			push(idx - 32)
		}
		if y < 31 {
			push(idx + 32)
		}
		if z > 0 {
			push(idx - 1024)
		}
		if z < 31 {
			push(idx + 1024)
		}
	}
	for w := range occ {
		occ[w] |= ^outside[w]
	}
}

// blocked walks the from..to segment through the voxel grid (Amanatides-Woo
// DDA) and reports whether it crosses an occupied voxel. Grid coordinates are
// (world - det)/12 + 16, so voxel v owns [v, v+1) and floor() is the index;
// anything else would misplace the cloud by half a voxel.
func (vs *voxelSmoke) blocked(from, to r3.Vector) bool {
	gx0 := (from.X-vs.det.X)/smokeVoxelSize + 16
	gy0 := (from.Y-vs.det.Y)/smokeVoxelSize + 16
	gz0 := (from.Z-vs.det.Z)/smokeVoxelSize + 16
	gx1 := (to.X-vs.det.X)/smokeVoxelSize + 16
	gy1 := (to.Y-vs.det.Y)/smokeVoxelSize + 16
	gz1 := (to.Z-vs.det.Z)/smokeVoxelSize + 16
	dx, dy, dz := gx1-gx0, gy1-gy0, gz1-gz0

	// clip to the occupied bounds, faces at [min, max+1]
	t0, t1 := 0.0, 1.0
	clip := func(p, d float64, lo, hi float64) bool {
		if d == 0 {
			return p >= lo && p <= hi
		}
		ta, tb := (lo-p)/d, (hi-p)/d
		if ta > tb {
			ta, tb = tb, ta
		}
		if ta > t0 {
			t0 = ta
		}
		if tb < t1 {
			t1 = tb
		}
		return t0 <= t1
	}
	if !clip(gx0, dx, float64(vs.min[0]), float64(vs.max[0]+1)) ||
		!clip(gy0, dy, float64(vs.min[1]), float64(vs.max[1]+1)) ||
		!clip(gz0, dz, float64(vs.min[2]), float64(vs.max[2]+1)) {
		return false
	}

	// walk from the entry point
	px, py, pz := gx0+dx*t0, gy0+dy*t0, gz0+dz*t0
	x, y, z := clampIdx(px, vs.min[0], vs.max[0]), clampIdx(py, vs.min[1], vs.max[1]), clampIdx(pz, vs.min[2], vs.max[2])
	stepX, tMaxX, tDeltaX := ddaAxis(gx0, dx, x)
	stepY, tMaxY, tDeltaY := ddaAxis(gy0, dy, y)
	stepZ, tMaxZ, tDeltaZ := ddaAxis(gz0, dz, z)
	for {
		idx := x | y<<5 | z<<10
		if vs.occ[idx>>6]>>(idx&63)&1 == 1 {
			return true
		}
		if tMaxX <= tMaxY && tMaxX <= tMaxZ {
			if tMaxX > t1 {
				return false
			}
			x += stepX
			if x < vs.min[0] || x > vs.max[0] {
				return false
			}
			tMaxX += tDeltaX
		} else if tMaxY <= tMaxZ {
			if tMaxY > t1 {
				return false
			}
			y += stepY
			if y < vs.min[1] || y > vs.max[1] {
				return false
			}
			tMaxY += tDeltaY
		} else {
			if tMaxZ > t1 {
				return false
			}
			z += stepZ
			if z < vs.min[2] || z > vs.max[2] {
				return false
			}
			tMaxZ += tDeltaZ
		}
	}
}

func clampIdx(g float64, lo, hi int) int {
	i := int(math.Floor(g))
	if i < lo {
		return lo
	}
	if i > hi {
		return hi
	}
	return i
}

// ddaAxis returns the step direction, the segment parameter t at which the
// walk first leaves voxel v along this axis, and the t advance per voxel.
func ddaAxis(g0, d float64, v int) (step int, tMax, tDelta float64) {
	if d > 0 {
		return 1, (float64(v+1) - g0) / d, 1 / d
	}
	if d < 0 {
		return -1, (float64(v) - g0) / d, -1 / d
	}
	return 0, math.Inf(1), math.Inf(1)
}
