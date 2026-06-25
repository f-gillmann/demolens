package parser

import (
	"github.com/f-gillmann/demolens/v2/model"
)

// roundEconomy sums each side's buy-window-close equipment value from the
// already-captured per-player RoundPlayer values (min(go-live+mp_buytime, death)).
// Called at finalize, after onKill and onBuyWindowClose have locked every value.
func roundEconomy(roster map[uint64]*model.RoundPlayer) model.RoundEconomy {
	ct, t := 0, 0
	for _, rp := range roster {
		switch rp.Side {
		case "CT":
			ct += rp.EquipmentValue
		case "T":
			t += rp.EquipmentValue
		}
	}
	return model.RoundEconomy{
		CT: model.TeamEconomy{EquipmentValue: ct, BuyType: buyType(ct)},
		T:  model.TeamEconomy{EquipmentValue: t, BuyType: buyType(t)},
	}
}

func buyType(value int) string {
	switch {
	case value < 5000:
		return "eco"
	case value < 10000:
		return "semi_eco"
	case value < 20000:
		return "semi_buy"
	default:
		return "full_buy"
	}
}
