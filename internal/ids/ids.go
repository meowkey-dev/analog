// Package ids implements the ID scheme.
//
// schema.sql specifies `s_<ulid>`, `c_<ulid>`, `l_<ulid>`, `a_<ulid>`; media reuses
// the same shape with `m_`. ULID over UUID4 because ids sort by creation time, which
// makes event logs and `ORDER BY id` readable without a separate timestamp column.
package ids

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	Space      = "s_"
	Card       = "c_"
	Link       = "l_"
	Annotation = "a_"
	Media      = "m_"
)

// One monotonic entropy source behind a mutex: several cards are commonly created
// inside a single millisecond, and two of them must not collide.
var (
	mu      sync.Mutex
	entropy = ulid.Monotonic(rand.Reader, 0)
)

func New(prefix string) string {
	mu.Lock()
	defer mu.Unlock()
	return prefix + ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

func SpaceID() string      { return New(Space) }
func CardID() string       { return New(Card) }
func LinkID() string       { return New(Link) }
func AnnotationID() string { return New(Annotation) }
func MediaID() string      { return New(Media) }
