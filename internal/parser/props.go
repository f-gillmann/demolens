package parser

import (
	"github.com/golang/geo/r3"
	st "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

// safePosition reads a raw entity's world position, guarding the CS2 ColorOrErr-
// style panic some entity types throw from Position(). Returns false when the
// position is unreadable (entity gone or never positioned).
func safePosition(e st.Entity) (v r3.Vector, ok bool) {
	if e == nil {
		return r3.Vector{}, false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return e.Position(), true
}

// Defensive raw prop readers. demoinfocs v5 stores props as untyped any and the
// concrete numeric type drifts across versions, where strict accessors would panic.

// propNum reads a prop as integer type T, converting from any integer concrete
// type. Float concrete values are rejected (use propF64 for those).
func propNum[T int | uint64](e st.Entity, name string) (T, bool) {
	if e == nil {
		return 0, false
	}
	v, ok := e.PropertyValue(name)
	if !ok {
		return 0, false
	}
	switch n := v.Any.(type) {
	case int32:
		return T(n), true
	case uint32:
		return T(n), true
	case int64:
		return T(n), true
	case uint64:
		return T(n), true
	default:
		return 0, false
	}
}

// propU64 reads a prop as a uint64, converting from any integer concrete type.
func propU64(e st.Entity, name string) (uint64, bool) { return propNum[uint64](e, name) }

// propI reads a prop as an int, converting from any integer concrete type.
func propI(e st.Entity, name string) (int, bool) { return propNum[int](e, name) }

// propF64 reads a prop as a float64, converting from any numeric concrete type.
func propF64(e st.Entity, name string) (float64, bool) {
	if e == nil {
		return 0, false
	}
	v, ok := e.PropertyValue(name)
	if !ok {
		return 0, false
	}
	switch n := v.Any.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int32:
		return float64(n), true
	case uint32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
