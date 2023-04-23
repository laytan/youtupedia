package store

import (
	"time"
)

func (t *Transcript) StartDuration() time.Duration {
	return time.Duration(t.Start) * time.Second
}
