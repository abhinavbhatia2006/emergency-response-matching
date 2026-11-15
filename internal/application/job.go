package application

type Job struct {
	ID        string `json:"id"`
	RequestID string `json:"request_id"`
	Version   uint64 `json:"-"`
}
