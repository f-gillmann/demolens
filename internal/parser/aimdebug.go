package parser

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/golang/geo/r3"
)

// aimDumper writes raw per-frame engagement-candidate rows and damage rows to CSV
// so an offline script can replay many time-to-damage / crosshair-placement
// algorithms without re-parsing. It is allocated only when Options.AimDebugPath is
// set; the three files open lazily on the first write of each kind. Steam ids are
// mapped to small per-match indices to keep the rows compact, and the legend file
// maps them back at close.
type aimDumper struct {
	path string

	candsW *bufio.Writer
	candsF *os.File
	dmgW   *bufio.Writer
	dmgF   *os.File

	ids   map[uint64]int // steam id -> small per-match index
	order []uint64       // index -> steam id, emitted as the legend at close
}

// newAimDumper allocates the dumper. The CSV files are not opened until the first
// row of their kind is written, so an enabled-but-silent parse leaves no files.
func newAimDumper(path string) *aimDumper {
	return &aimDumper{path: path, ids: map[uint64]int{}}
}

// idx returns the small per-match index for a steam id, assigning the next one on
// first sight.
func (d *aimDumper) idx(steamID uint64) int {
	if i, ok := d.ids[steamID]; ok {
		return i
	}
	i := len(d.order)
	d.ids[steamID] = i
	d.order = append(d.order, steamID)
	return i
}

// openCands lazily opens the cands CSV and writes its header.
func (d *aimDumper) openCands() bool {
	if d.candsW != nil {
		return true
	}
	f, err := os.Create(d.path + ".cands.csv")
	if err != nil {
		return false
	}
	d.candsF = f
	d.candsW = bufio.NewWriterSize(f, 1<<20)
	d.candsW.WriteString("round,t_ms,tick,shooter,victim,vx,vy,vz,ang_off,vis_n,vis_d,vis_torso,spotted,dist,dz,v_duck,s_scoped,s_blind,s_blindfrac,s_weap,fire_blk,body_blk,vis_geo\n")
	return true
}

// openDmg lazily opens the dmg CSV and writes its header.
func (d *aimDumper) openDmg() bool {
	if d.dmgW != nil {
		return true
	}
	f, err := os.Create(d.path + ".dmg.csv")
	if err != nil {
		return false
	}
	d.dmgF = f
	d.dmgW = bufio.NewWriterSize(f, 1<<20)
	d.dmgW.WriteString("round,t_ms,tick,attacker,victim,weapon,is_gun,hitgroup,dmg\n")
	return true
}

// cand writes one engagement-candidate row: shooter view vector, the angle off the
// enemy, three visibility verdicts (narrow 9-ray, dense any-part, strict torso
// column), the engine spotted flag (shooter spotted victim), the eye-to-eye distance
// and height delta, the victim-duck / shooter-scoped / shooter-weapon state, the
// two measurement-only occluder probes (sightline through fire, through a body), and
// the geometry-only any-part verdict with no smoke/fire/body gating (visGeo).
func (d *aimDumper) cand(round int, tMs int64, tick int, sID, vID uint64, v r3.Vector, angOff float64, visN, visD, visTorso, spotted bool, dist, dz float64, vDuck, sScoped bool, sBlind, sBlindFrac float64, sWeap string, fireBlk, bodyBlk, visGeo bool) {
	if !d.openCands() {
		return
	}
	w := d.candsW
	w.WriteString(strconv.Itoa(round))
	w.WriteByte(',')
	w.WriteString(strconv.FormatInt(tMs, 10))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(tick))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(d.idx(sID)))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(d.idx(vID)))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(v.X, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(v.Y, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(v.Z, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(angOff, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteByte(boolByte(visN))
	w.WriteByte(',')
	w.WriteByte(boolByte(visD))
	w.WriteByte(',')
	w.WriteByte(boolByte(visTorso))
	w.WriteByte(',')
	w.WriteByte(boolByte(spotted))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(dist, 'f', 1, 64))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(dz, 'f', 1, 64))
	w.WriteByte(',')
	w.WriteByte(boolByte(vDuck))
	w.WriteByte(',')
	w.WriteByte(boolByte(sScoped))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(sBlind, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteString(strconv.FormatFloat(sBlindFrac, 'g', -1, 64))
	w.WriteByte(',')
	w.WriteString(strings.ReplaceAll(sWeap, ",", ""))
	w.WriteByte(',')
	w.WriteByte(boolByte(fireBlk))
	w.WriteByte(',')
	w.WriteByte(boolByte(bodyBlk))
	w.WriteByte(',')
	w.WriteByte(boolByte(visGeo))
	w.WriteByte('\n')
}

// dmg writes one damage row with the RAW pre-clamp health damage.
func (d *aimDumper) dmg(round int, tMs int64, tick int, aID, vID uint64, weapon string, isGun bool, hitgroup string, dmg int) {
	if !d.openDmg() {
		return
	}
	w := d.dmgW
	w.WriteString(strconv.Itoa(round))
	w.WriteByte(',')
	w.WriteString(strconv.FormatInt(tMs, 10))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(tick))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(d.idx(aID)))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(d.idx(vID)))
	w.WriteByte(',')
	w.WriteString(csvField(weapon))
	w.WriteByte(',')
	w.WriteByte(boolByte(isGun))
	w.WriteByte(',')
	w.WriteString(csvField(hitgroup))
	w.WriteByte(',')
	w.WriteString(strconv.Itoa(dmg))
	w.WriteByte('\n')
}

// close flushes both data writers, writes the legend, and closes every file. The
// first error seen is returned; later steps still run so nothing is left open.
func (d *aimDumper) close() error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if d.candsW != nil {
		keep(d.candsW.Flush())
		keep(d.candsF.Close())
	}
	if d.dmgW != nil {
		keep(d.dmgW.Flush())
		keep(d.dmgF.Close())
	}
	if len(d.order) > 0 {
		if f, err := os.Create(d.path + ".legend.csv"); err != nil {
			keep(err)
		} else {
			w := bufio.NewWriter(f)
			w.WriteString("index,steam_id\n")
			for i, id := range d.order {
				w.WriteString(strconv.Itoa(i))
				w.WriteByte(',')
				w.WriteString(strconv.FormatUint(id, 10))
				w.WriteByte('\n')
			}
			keep(w.Flush())
			keep(f.Close())
		}
	}
	return firstErr
}

// boolByte renders a bool as the byte '1' or '0' for the 0/1 CSV columns.
func boolByte(b bool) byte {
	if b {
		return '1'
	}
	return '0'
}

// csvField quotes a string only when it carries a comma, quote or newline, so the
// common (safe) weapon / hitgroup names pass through untouched.
func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}
