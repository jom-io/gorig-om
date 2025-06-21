package errstat

type ErrStat struct {
	At    int64 `json:"at"`    // Timestamp of the error
	Warn  int64 `json:"warn"`  // Warning count
	Error int64 `json:"error"` // Error count
	Panic int64 `json:"panic"` // Panic count
	Total int64 `json:"total"` // Total count of errors
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
