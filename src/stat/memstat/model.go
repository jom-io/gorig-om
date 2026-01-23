package memstat

type BigObjStat struct {
	At           int64  `json:"at"`
	Key          string `json:"key"`
	Func         string `json:"func"`
	File         string `json:"file"`
	Line         int64  `json:"line"`
	InuseSpace   int64  `json:"inuseSpace"`
	InuseObjects int64  `json:"inuseObjects"`
	AvgObjSize   int64  `json:"avgObjSize"`
}

type BigObjRank struct {
	Func         string `json:"func"`
	File         string `json:"file"`
	Line         int64  `json:"line"`
	InuseSpace   int64  `json:"inuseSpace"`
	InuseObjects int64  `json:"inuseObjects"`
	AvgObjSize   int64  `json:"avgObjSize"`
	LastAt       int64  `json:"lastAt"`
}

type LeakPoint struct {
	Func         string `json:"func"`
	File         string `json:"file"`
	Line         int64  `json:"line"`
	DeltaSpace   int64  `json:"deltaSpace"`
	DeltaObjects int64  `json:"deltaObjects"`
	AvgObjSize   int64  `json:"avgObjSize"`
}

type LeakEvent struct {
	At              int64       `json:"at"`
	AllocBytes      uint64      `json:"allocBytes"`
	ObjectCount     uint64      `json:"objectCount"`
	AllocDelta      uint64      `json:"allocDelta"`
	ObjectDelta     uint64      `json:"objectDelta"`
	BaseInuseSpace  int64       `json:"baseInuseSpace"`
	LeakInuseSpace  int64       `json:"leakInuseSpace"`
	BaseInuseObject int64       `json:"baseInuseObject"`
	LeakInuseObject int64       `json:"leakInuseObject"`
	BaseProfile     string      `json:"baseProfile"`
	LeakProfile     string      `json:"leakProfile"`
	Points          []LeakPoint `json:"points"`
}
