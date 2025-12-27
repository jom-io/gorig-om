package errstat

type ErrStat struct {
	At    int64 `json:"at"`    // Timestamp of the error
	Warn  int64 `json:"warn"`  // Warning count
	Error int64 `json:"error"` // Error count
	Panic int64 `json:"panic"` // Panic count
	Total int64 `json:"total"` // Total count of errors
}

type ErrSigStat struct {
	At      int64  `json:"at"`      // Timestamp in seconds (minute bucket)
	Level   string `json:"level"`   // Log level
	SigHash string `json:"sigHash"` // Hash of signature for grouping/index
	Count   int64  `json:"count"`   // Occurrences in the bucket
}

type ErrSigMeta struct {
	SigHash     string `json:"sigHash"`               // Hash of signature for grouping/index
	Signature   string `json:"signature"`             // Normalized exception signature
	Level       string `json:"level"`                 // Log level bound to this signature
	SampleMsg   string `json:"sampleMsg,omitempty"`   // Sample msg
	SampleError string `json:"sampleError,omitempty"` // Sample error
	SampleTrace string `json:"sampleTrace,omitempty"` // Sample trace ID
	FirstAt     int64  `json:"firstAt"`               // First seen time (unix seconds)
	LastAt      int64  `json:"lastAt"`                // Last seen time (unix seconds)
}

type ErrSigRank struct {
	SigHash     string `json:"sigHash"`
	Signature   string `json:"signature"`
	Level       string `json:"level"`
	Count       int64  `json:"count"`
	SampleMsg   string `json:"sampleMsg,omitempty"`
	SampleError string `json:"sampleError,omitempty"`
	SampleTrace string `json:"sampleTrace,omitempty"`
	FirstAt     int64  `json:"firstAt"`
	LastAt      int64  `json:"lastAt"`
}

type ErrType string

const (
	ErrTypeWarn  ErrType = "warn"  // Warning error type
	ErrTypeError ErrType = "error" // Error type
	ErrTypePanic ErrType = "panic" // Panic error type
	ErrTypeTotal ErrType = "total" // Total error type
)

func (r ErrType) String() string {
	return string(r)
}
