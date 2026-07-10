package geom

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golang/geo/r3"
)

// magic at the head of every .tri file: "DLT" then format version 1.
var triMagic = [4]byte{'D', 'L', 'T', '1'}

// Mesh is one map's static collision, with a BVH on top for fast occlusion.
type Mesh struct {
	tris  []triangle
	nodes []bvhNode
	order []int // triangle indices grouped by leaf
}

// e1/e2 are b-a and c-a, precomputed once at Load: the mesh is static, so these
// never change and would otherwise be recomputed on every blocks() ray test.
type triangle struct {
	a, b, c r3.Vector
	e1, e2  r3.Vector
}

// MapFile returns where a map's collision file lives. Workshop maps key off the
// addon id since two remakes can share a name; official maps key off the name.
func MapFile(dir, workshopID, mapName string) string {
	key := mapName
	if workshopID != "" {
		key = workshopID
	}
	return filepath.Join(dir, key+".tri")
}

// Load reads a .tri mesh off disk and builds its BVH.
func Load(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, fmt.Errorf("geom: read magic: %w", err)
	}
	if magic != triMagic {
		return nil, fmt.Errorf("geom: %s: not a .tri file", path)
	}

	var count uint32
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("geom: read count: %w", err)
	}

	verts := make([]float32, 9*count)
	if err := binary.Read(f, binary.LittleEndian, verts); err != nil {
		return nil, fmt.Errorf("geom: read triangles: %w", err)
	}

	m := &Mesh{tris: make([]triangle, count)}
	for i := range m.tris {
		v := verts[i*9:]
		a := r3.Vector{X: float64(v[0]), Y: float64(v[1]), Z: float64(v[2])}
		b := r3.Vector{X: float64(v[3]), Y: float64(v[4]), Z: float64(v[5])}
		c := r3.Vector{X: float64(v[6]), Y: float64(v[7]), Z: float64(v[8])}
		m.tris[i] = triangle{a: a, b: b, c: c, e1: b.Sub(a), e2: c.Sub(a)}
	}

	m.buildBVH()
	return m, nil
}

// WriteTri writes triangles out in the DLT1 format Load expects. Each triangle
// is 9 floats: ax,ay,az, bx,by,bz, cx,cy,cz. game units, Z-up.
func WriteTri(path string, tris [][9]float32) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	if _, err := w.Write(triMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(tris))); err != nil {
		return err
	}
	for i := range tris {
		if err := binary.Write(w, binary.LittleEndian, tris[i][:]); err != nil {
			return err
		}
	}
	return w.Flush()
}

const epsilon = 1e-7

// Moeller-Trumbore: does the segment orig+dir (t in 0..1) hit this triangle.
func (t triangle) blocks(orig, dir r3.Vector) bool {
	edge1 := t.e1
	edge2 := t.e2
	pvec := dir.Cross(edge2)
	det := edge1.Dot(pvec)
	if det > -epsilon && det < epsilon {
		return false // parallel, no hit
	}
	inv := 1.0 / det

	tvec := orig.Sub(t.a)
	u := tvec.Dot(pvec) * inv
	if u < 0 || u > 1 {
		return false
	}

	qvec := tvec.Cross(edge1)
	v := dir.Dot(qvec) * inv
	if v < 0 || u+v > 1 {
		return false
	}

	hit := edge2.Dot(qvec) * inv
	// strictly inside the segment, endpoints don't count (shooter/target aren't walls)
	return hit > epsilon && hit < 1-epsilon
}
