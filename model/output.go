package model

// OutputMeta is the self-describing fidelity descriptor: the tier and the set of
// populated detail streams. Lives at meta.output.
type OutputMeta struct {
	Tier              string   `json:"tier"`                          // core / detail / full
	Streams           []string `json:"streams"`                       // populated stream names, sorted
	PositionsSampleHz float64  `json:"positions_sample_hz,omitempty"` // declared frame cadence, default 4.0
	MapMeshLoaded     bool     `json:"map_mesh_loaded"`               // were geometric LOS aim stats computable
}
