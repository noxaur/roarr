package server

import (
	"testing"
	"testing/fstest"
)

// Registering overlapping patterns panics; build the mux once to catch it.
func TestRoutesRegister(t *testing.T) {
	s := &Server{ui: fstest.MapFS{}}
	_ = s.Handler()
}
