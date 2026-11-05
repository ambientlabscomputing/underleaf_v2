package cluster

import (
	"fmt"
	"sync/atomic"
)

type atomicString struct{ v atomic.Value }

func (a *atomicString) set(s string) { a.v.Store(s) }
func (a *atomicString) get() string {
	s, _ := a.v.Load().(string)
	return s
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
