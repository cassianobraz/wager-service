package ports

import "errors"

var ErrOptimisticLock = errors.New("ports: optimistic concurrency conflict")
