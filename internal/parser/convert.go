package parser

import (
	"sort"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// wrapDeg folds a degree delta into [-180, 180] so yaw diffs across the +/-180
// seam don't blow up.
func wrapDeg(d float64) float64 {
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	return d
}

// grenadeTypeString is the model-facing name for a grenade type.
func grenadeTypeString(t common.EquipmentType) string {
	switch t {
	case common.EqFlash:
		return "flash"
	case common.EqSmoke:
		return "smoke"
	case common.EqHE:
		return "he"
	case common.EqMolotov:
		return "molotov"
	case common.EqIncendiary:
		return "incendiary"
	case common.EqDecoy:
		return "decoy"
	default:
		return ""
	}
}

// demoinfocs world vector to our Position
func toPosition(v r3.Vector) model.Position {
	return model.Position{X: v.X, Y: v.Y, Z: v.Z}
}

func grenadePosition(g *common.GrenadeProjectile) model.Position {
	if g == nil {
		return model.Position{}
	}
	return toPosition(g.Position())
}

// grenadeInventoryValue is the dollar value of nades a player is holding.
// Non-grenades fall through utilityPrice as 0.
func grenadeInventoryValue(p *common.Player) int {
	value := 0
	for _, w := range p.Weapons() {
		value += csdata.UtilityPrice[w.Type]
	}
	return value
}

// toFlash projects the working grenade onto the typed flash bucket entry.
func (g *parseGrenade) toFlash() model.GrenadeFlash {
	sortFlashed(g.flashed)
	return model.GrenadeFlash{
		GrenadeID:        g.grenadeID,
		Thrower:          g.thrower,
		Side:             g.side,
		Type:             "flash",
		ThrowMs:          g.throwT,
		DetonateMs:       g.detonateT,
		FlightMs:         g.flightMs,
		ThrowPosition:    g.throwPosition,
		DetonatePosition: g.detonatePosition,
		EnemiesFlashed:   g.enemiesFlashed,
		TeammatesFlashed: g.teammatesFlashed,
		Flashed:          g.flashed,
	}
}

// toHE projects the working grenade onto the typed HE bucket entry.
func (g *parseGrenade) toHE() model.GrenadeHE {
	sortVictims(g.victims)
	return model.GrenadeHE{
		GrenadeID:        g.grenadeID,
		Thrower:          g.thrower,
		Side:             g.side,
		Type:             "he",
		ThrowMs:          g.throwT,
		DetonateMs:       g.detonateT,
		FlightMs:         g.flightMs,
		ThrowPosition:    g.throwPosition,
		DetonatePosition: g.detonatePosition,
		DamageDealt:      g.damageDealt,
		TeamDamage:       g.teamDamage,
		Victims:          g.victims,
	}
}

// toMolotov projects the working grenade onto the typed molotov bucket entry. The
// type is preserved as "molotov" or "incendiary" (the bucket folds both).
func (g *parseGrenade) toMolotov() model.GrenadeMolotov {
	sortVictims(g.victims)
	return model.GrenadeMolotov{
		GrenadeID:        g.grenadeID,
		Thrower:          g.thrower,
		Side:             g.side,
		Type:             g.gtype,
		ThrowMs:          g.throwT,
		DetonateMs:       g.detonateT,
		ExpireMs:         g.expireT,
		FlightMs:         g.flightMs,
		ThrowPosition:    g.throwPosition,
		DetonatePosition: g.detonatePosition,
		DamageDealt:      g.damageDealt,
		TeamDamage:       g.teamDamage,
		Victims:          g.victims,
		FireCells:        g.fireCells,
	}
}

// toBasic projects the working grenade onto the typed smoke/decoy bucket entry.
func (g *parseGrenade) toBasic() model.GrenadeBasic {
	return model.GrenadeBasic{
		GrenadeID:        g.grenadeID,
		Thrower:          g.thrower,
		Side:             g.side,
		Type:             g.gtype,
		ThrowMs:          g.throwT,
		DetonateMs:       g.detonateT,
		ExpireMs:         g.expireT,
		FlightMs:         g.flightMs,
		ThrowPosition:    g.throwPosition,
		DetonatePosition: g.detonatePosition,
		Voxels:           g.voxels,
	}
}

// weaponValue is the dollar value to report for a held gun. A bought gun reads
// its WeaponPrice; picked-up guns and the free spawn pistol read 0. holder is
// the current owner's SteamID64.
func weaponValue(w *common.Equipment, holder uint64) int {
	if w == nil {
		return 0
	}

	price := csdata.WeaponPrice[w.Type]
	orig := weaponOriginalOwner(w)

	// picked up from someone else: not this player's buy.
	if orig != 0 && orig != holder {
		return 0
	}
	// a pistol with no recorded original owner is the free spawn USP/Glock.
	if orig == 0 && w.Class() == common.EqClassPistols {
		return 0
	}

	return price
}

// weaponOriginalOwner reads the gun's first owner from the raw owner-chain props.
// m_OriginalOwnerXuidLow/High combine into a SteamID64. 0 when absent (omitempty).
func weaponOriginalOwner(w *common.Equipment) uint64 {
	if w == nil || w.Entity == nil {
		return 0
	}
	low, lok := propU64(w.Entity, "m_OriginalOwnerXuidLow")
	high, hok := propU64(w.Entity, "m_OriginalOwnerXuidHigh")
	if !lok || !hok {
		return 0
	}
	return high<<32 | low
}

// weaponPrevOwner reads the gun's previous holder from the m_hPrevOwner entity
// handle and resolves it to a SteamID64. 0 when absent or unresolvable.
func (st *parseState) weaponPrevOwner(w *common.Equipment) uint64 {
	if w == nil || w.Entity == nil {
		return 0
	}

	h, ok := propU64(w.Entity, "m_hPrevOwner")
	if !ok || h == 0 {
		return 0
	}

	pl := st.parsed.GameState().Participants().FindByPawnHandle(h)
	if pl == nil {
		return 0
	}

	return pl.SteamID64
}

// equipmentPosition is a dropped weapon's world position, or nil if the entity is
// gone (so the field is omitted rather than emitted as the origin).
func equipmentPosition(w *common.Equipment) *model.Position {
	if w == nil || w.Entity == nil {
		return nil
	}
	p := toPosition(w.Entity.Position())
	return &p
}

// heldItem is one accumulated inventory slot: the equipment and how many of that
// type the player holds.
type heldItem struct {
	w     *common.Equipment
	count int
}

// addHeld bumps the count for w's type in m, seeding the slot when it is new.
func addHeld(m map[common.EquipmentType]*heldItem, w *common.Equipment, n int) {
	if a := m[w.Type]; a != nil {
		a.count += n
		return
	}
	m[w.Type] = &heldItem{w: w, count: n}
}

// classifyHeldItems buckets a player's held items into weapons, grenades and
// equipment by class. Only flashbangs stack (via FlashbangCount); AmmoReserve is 0
// for grenades on source 2.
func classifyHeldItems(pl *common.Player) (weapons, grenades, equipment map[common.EquipmentType]*heldItem) {
	weapons = map[common.EquipmentType]*heldItem{}
	grenades = map[common.EquipmentType]*heldItem{}
	equipment = map[common.EquipmentType]*heldItem{}
	for _, w := range pl.Weapons() {
		switch w.Class() {
		case common.EqClassPistols, common.EqClassSMG, common.EqClassHeavy, common.EqClassRifle:
			addHeld(weapons, w, 1)
		case common.EqClassGrenade:
			held := 1
			if w.Type == common.EqFlash {
				if fc := int(pl.FlashbangCount()); fc > held {
					held = fc
				}
			}
			addHeld(grenades, w, held)
		default: // knife, zeus, armor, defuse kit, misc equipment
			addHeld(equipment, w, 1)
		}
	}
	return weapons, grenades, equipment
}

// loadoutWeapons turns the gun buckets into sorted model weapons.
func (st *parseState) loadoutWeapons(weapons map[common.EquipmentType]*heldItem, owner uint64) []model.LoadoutWeapon {
	var out []model.LoadoutWeapon
	for t, a := range weapons {
		out = append(out, model.LoadoutWeapon{
			Name:          t.String(),
			Class:         csdata.EquipmentClassName(t),
			Count:         a.count,
			Value:         weaponValue(a.w, owner),
			OriginalOwner: weaponOriginalOwner(a.w),
			PrevOwner:     st.weaponPrevOwner(a.w),
		})
	}
	// Total-order tie-break so the map-built order is deterministic.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.OriginalOwner != b.OriginalOwner {
			return a.OriginalOwner < b.OriginalOwner
		}
		return a.PrevOwner < b.PrevOwner
	})
	return out
}

// loadoutGrenades turns the grenade buckets into sorted model items.
func loadoutGrenades(grenades map[common.EquipmentType]*heldItem) []model.LoadoutItem {
	var out []model.LoadoutItem
	for t, a := range grenades {
		out = append(out, model.LoadoutItem{
			Name:  t.String(),
			Count: a.count,
			Value: csdata.UtilityPrice[t] * a.count,
		})
	}
	// Total-order tie-break so the map-built order is deterministic.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		return a.Count > b.Count
	})
	return out
}

// loadoutEquipment turns knives/armor/kits into sorted model items, adding the
// defuse kit, which is a player flag rather than a weapon entity.
func loadoutEquipment(equipment map[common.EquipmentType]*heldItem, hasDefuseKit bool) []model.LoadoutItem {
	var out []model.LoadoutItem
	for t, a := range equipment {
		val := 0
		if t == common.EqKevlar || t == common.EqHelmet {
			val = csdata.WeaponPrice[t]
		}
		out = append(out, model.LoadoutItem{Name: t.String(), Count: a.count, Value: val})
	}
	if hasDefuseKit {
		out = append(out, model.LoadoutItem{Name: common.EqDefuseKit.String(), Count: 1, Value: 0})
	}
	// Total-order tie-break so the map-built order is deterministic.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		return a.Count > b.Count
	})
	return out
}

// buildLoadout groups a player's held items into the typed Loadout. TotalValue
// mirrors EquipmentValueFreezeTimeEnd; it is NOT a sum of the item values.
func (st *parseState) buildLoadout(pl *common.Player) model.Loadout {
	weapons, grenades, equipment := classifyHeldItems(pl)
	return model.Loadout{
		TotalValue: pl.EquipmentValueFreezeTimeEnd(),
		Weapons:    st.loadoutWeapons(weapons, pl.SteamID64),
		Grenades:   loadoutGrenades(grenades),
		Equipment:  loadoutEquipment(equipment, pl.HasDefuseKit()),
	}
}

// inventorySnapshot builds one inventory-change-log entry for a player at a
// phase: the grouped buildLoadout buckets plus live health/armor/money/active
// weapon. The caller owns phase choice and dedup gating.
func (st *parseState) inventorySnapshot(pl *common.Player, side, phase string, into int64) model.InventoryChange {
	lo := st.buildLoadout(pl)
	ic := model.InventoryChange{
		SteamID:        pl.SteamID64,
		Side:           side,
		Phase:          phase,
		TMs:            into,
		Health:         pl.Health(),
		Armor:          pl.Armor(),
		HasHelmet:      pl.HasHelmet(),
		HasDefuseKit:   pl.HasDefuseKit(),
		Money:          pl.Money(),
		EquipmentValue: pl.EquipmentValueCurrent(),
		Weapons:        lo.Weapons,
		Grenades:       lo.Grenades,
		Equipment:      lo.Equipment,
	}
	if w := pl.ActiveWeapon(); w != nil {
		ic.ActiveWeapon = w.String()
	}
	return ic
}
