package apistat

type StatusAgg struct {
	Count int64 `json:"count"`
	Sum   int64 `json:"sum"` // total latency ms
	Max   int64 `json:"max"` // max latency ms
}

// ApiLatencyStat stores per-minute latency aggregation for a method+uri.
type ApiLatencyStat struct {
	At              int64  `json:"at"`         // minute bucket, unix seconds
	Method          string `json:"method"`     // HTTP method
	URI             string `json:"uri"`        // normalized path
	Count           int64  `json:"count"`      // total requests
	SumLatency      int64  `json:"sumLatency"` // total latency ms
	MaxLatency      int64  `json:"maxLatency"` // max latency ms
	Count2xx        int64  `json:"count2xx"`
	SumLatency2xx   int64  `json:"sumLatency2xx"`
	MaxLatency2xx   int64  `json:"maxLatency2xx"`
	Count4xx        int64  `json:"count4xx"`
	SumLatency4xx   int64  `json:"sumLatency4xx"`
	MaxLatency4xx   int64  `json:"maxLatency4xx"`
	Count5xx        int64  `json:"count5xx"`
	SumLatency5xx   int64  `json:"sumLatency5xx"`
	MaxLatency5xx   int64  `json:"maxLatency5xx"`
	CountOther      int64  `json:"countOther"`
	SumLatencyOther int64  `json:"sumLatencyOther"`
	MaxLatencyOther int64  `json:"maxLatencyOther"`
}

// Meta for samples.
type ApiLatencyMeta struct {
	Method       string `json:"method"`
	URI          string `json:"uri"`
	SampleTrace  string `json:"sampleTrace,omitempty"`
	SampleStatus int    `json:"sampleStatus,omitempty"`
	FirstAt      int64  `json:"firstAt"`
	LastAt       int64  `json:"lastAt"`
}

type ApiLatencyRank struct {
	Method      string `json:"method"`
	URI         string `json:"uri"`
	Count       int64  `json:"count"`
	AvgLatency  int64  `json:"avgLatency"`
	MaxLatency  int64  `json:"maxLatency"`
	Count2xx    int64  `json:"count2xx"`
	Count4xx    int64  `json:"count4xx"`
	Count5xx    int64  `json:"count5xx"`
	CountOther  int64  `json:"countOther"`
	SampleTrace string `json:"sampleTrace,omitempty"`
}
