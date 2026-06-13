package parser

import (
	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// roundEconomy snapshots each side's buy as the round goes live.
func roundEconomy(gs dem.GameState) model.RoundEconomy {
	return model.RoundEconomy{
		CT: teamEconomy(gs.Participants().TeamMembers(common.TeamCounterTerrorists)),
		T:  teamEconomy(gs.Participants().TeamMembers(common.TeamTerrorists)),
	}
}

func teamEconomy(members []*common.Player) model.TeamEconomy {
	value := 0
	for _, p := range members {
		value += p.EquipmentValueCurrent()
	}
	return model.TeamEconomy{EquipmentValue: value, BuyType: buyType(value)}
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
