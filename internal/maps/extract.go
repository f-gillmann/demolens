package maps

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Params is one extraction. Pick exactly one source: In (a glb/obj you exported),
// VPK (a map .vpk), or CS2Dir + Map
type Params struct {
	In     string // already-exported .glb/.gltf/.obj, skips Source2Viewer
	VPK    string // map .vpk to extract
	CS2Dir string // CS2 install dir, resolves official maps together with Map
	Map    string // official map name, e.g. de_mirage
	VRF    string // source2viewer-cli path, defaults to "source2viewer-cli"
	Key    string // output filename key (workshop id or map name), defaults to Map
	OutDir string // where <Key>.tri lands, defaults to "tris"
}

// Extract writes <OutDir>/<Key>.tri for a map and returns the path and triangle
// count. Best-effort across platforms: it shells out to Source2Viewer-CLI and
// reads back the glTF.
func Extract(p Params) (string, int, error) {
	if p.VRF == "" {
		p.VRF = "source2viewer-cli"
	}
	if p.OutDir == "" {
		p.OutDir = "tris"
	}

	infile := p.In
	if infile == "" {
		vpk := p.VPK
		if vpk == "" && p.CS2Dir != "" && p.Map != "" {
			vpk = filepath.Join(p.CS2Dir, "game", "csgo", "maps", p.Map+".vpk")
		}
		if vpk == "" {
			return "", 0, errors.New("maps: need In, or VPK, or CS2Dir with Map")
		}
		tmp, err := os.MkdirTemp("", "demolens-vrf-")
		if err != nil {
			return "", 0, err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		if infile, err = runVRF(p.VRF, vpk, tmp); err != nil {
			return "", 0, err
		}
	}

	key := p.Key
	if key == "" {
		key = p.Map
	}
	if key == "" {
		return "", 0, errors.New("maps: need Key (or Map) for the output filename")
	}

	var tris [][9]float32
	var err error
	if strings.HasSuffix(strings.ToLower(infile), ".obj") {
		tris, err = trisFromOBJ(infile)
	} else {
		tris, err = trisFromGLB(infile)
	}
	if err != nil {
		return "", 0, err
	}
	if len(tris) == 0 {
		return "", 0, errors.New("maps: no triangles extracted")
	}
	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return "", 0, err
	}
	outPath := filepath.Join(p.OutDir, key+".tri")
	if err := geom.WriteTri(outPath, tris); err != nil {
		return "", 0, err
	}
	return outPath, len(tris), nil
}

// runVRF decompiles the .vpk and exports its physics mesh to glb, handing back
// the *_physics.glb path. heads up: VRF flags drift between releases.
func runVRF(vrf, vpk, workdir string) (string, error) {
	mapBase := strings.TrimSuffix(filepath.Base(vpk), filepath.Ext(vpk))
	cmd := exec.Command(vrf, "-i", vpk, "-d",
		"-f", "maps/"+mapBase+"/world_physics.vmdl_c",
		"--gltf_export_format", "glb", "-o", workdir)
	cmd.Stderr = os.Stderr
	_, _ = fmt.Fprintln(os.Stderr, "running:", cmd.String())
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("maps: source2viewer-cli failed (%w); check the VRF path / flags for your version", err)
	}

	// VRF drops a tiny empty stub (world_physics.glb) next to the real mesh
	// (world_physics_physics.glb). grab the biggest .glb, that's the geometry.
	var best string
	var bestSize int64 = -1
	_ = filepath.WalkDir(workdir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(p), ".glb") {
			return nil
		}
		if info, _ := d.Info(); info != nil && info.Size() > bestSize {
			best, bestSize = p, info.Size()
		}
		return nil
	})

	if best == "" {
		return "", errors.New("maps: Source2Viewer produced no glTF; check flags for your version")
	}
	return best, nil
}

// trisFromGLB reads a glTF/GLB into triangles, kept in the demo's coordinate
// space. VRF stores physics verts in game units (Source Z-up), so we take the
// raw accessor positions and deliberately skip node transforms. applying them
// would rescale to glTF meters/Y-up and break every distance check downstream.
func trisFromGLB(path string) ([][9]float32, error) {
	doc, err := gltf.Open(path)
	if err != nil {
		return nil, err
	}

	var tris [][9]float32
	for _, m := range doc.Meshes {
		// this mesh is only for los, so drop physics groups that don't stop vision:
		//   clip brushes (player/npc/grenade/ladder): invisible volumes, the
		//     playerclip is basically a map-sized box that would block every sightline.
		//   physics_sky: the skybox shell (de_cache etc).
		//   chainlink: wire fences you see right through (overpass).
		// glass stays. CS2 windows start solid and we don't track when they break,
		// so opaque is the safe default. solid physics_group_* and passbullets_*
		// walls stay too.
		if name := strings.ToLower(m.Name); strings.Contains(name, "clip") ||
			strings.Contains(name, "chainlink") || strings.Contains(name, "sky") {
			continue
		}

		for _, prim := range m.Primitives {
			posIdx, ok := prim.Attributes[gltf.POSITION]
			if !ok {
				continue
			}
			pos, err := modeler.ReadPosition(doc, doc.Accessors[posIdx], nil)
			if err != nil {
				return nil, err
			}

			var idx []uint32
			if prim.Indices != nil {
				if idx, err = modeler.ReadIndices(doc, doc.Accessors[*prim.Indices], nil); err != nil {
					return nil, err
				}
			} else {
				idx = make([]uint32, len(pos))
				for i := range idx {
					idx[i] = uint32(i)
				}
			}

			for i := 0; i+2 < len(idx); i += 3 {
				a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
				tris = append(tris, [9]float32{a[0], a[1], a[2], b[0], b[1], b[2], c[0], c[1], c[2]})
			}
		}
	}
	return tris, nil
}

// trisFromOBJ reads an OBJ into triangles, fan-triangulating any polygon faces.
func trisFromOBJ(path string) ([][9]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var verts [][3]float32
	var tris [][9]float32
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "v":
			if len(fields) >= 4 {
				x, _ := strconv.ParseFloat(fields[1], 32)
				y, _ := strconv.ParseFloat(fields[2], 32)
				z, _ := strconv.ParseFloat(fields[3], 32)
				verts = append(verts, [3]float32{float32(x), float32(y), float32(z)})
			}
		case "f":
			idx := make([]int, 0, len(fields)-1)
			for _, p := range fields[1:] {
				n, err := strconv.Atoi(strings.SplitN(p, "/", 2)[0])
				if err != nil {
					continue
				}
				if n < 0 {
					n = len(verts) + n // negative = relative to end
				} else {
					n-- // OBJ indices start at 1
				}
				idx = append(idx, n)
			}

			for k := 1; k+1 < len(idx); k++ {
				a, b, c := verts[idx[0]], verts[idx[k]], verts[idx[k+1]]
				tris = append(tris, [9]float32{a[0], a[1], a[2], b[0], b[1], b[2], c[0], c[1], c[2]})
			}
		}
	}
	return tris, sc.Err()
}
