package counter

// A CountCallback is called whenever some data is written or read and the byte count changes
type CountCallback func(count int64)

// A Counter counts how much data flows through it, in bytes
type Counter interface{}
