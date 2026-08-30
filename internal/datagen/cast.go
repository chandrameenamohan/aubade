package datagen

import "github.com/chandrameenamohan/aubade/internal/model"

// The cast. Every name here comes from the assignment's profile.md appendix —
// the people it says "show up in the data" — plus the handful the sample digest
// names (David Kim at Aperture, Renee Tan, the Lumen rep). Nobody
// else exists, because a corpus full of extras dilutes the exam without adding
// a question to it.
//
// Addresses are fictional by construction: everything external sits on a
// .example domain except the two the assignment itself writes down
// (inflectionpoint.vc, wsgr.com), and everything internal sits on tessera.io.
var (
	// Avery Chen — the user. Every digest is about her morning.
	Avery = model.Person{Name: "Avery Chen", Email: "avery@tessera.io"}

	// Household. "Anything from Sam is P0. Personal." — and never draft for Sam.
	Sam = model.Person{Name: "Sam Park", Email: "sam@parkhouse.example"}

	// Tessera, the 12-person company.
	Priya  = model.Person{Name: "Priya Iyer", Email: "priya@tessera.io"}
	Jordan = model.Person{Name: "Jordan Liu", Email: "jordan@tessera.io"}
	Tomas  = model.Person{Name: "Tomás Reyes", Email: "tomas@tessera.io"}
	Nadia  = model.Person{Name: "Nadia Boulos", Email: "nadia@tessera.io"}

	// The raise.
	Marcus = model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"}
	David  = model.Person{Name: "David Kim", Email: "david@aperturecapital.example"}
	Diane  = model.Person{Name: "Diane Okafor", Email: "diane@okaforcapital.example"}
	Ben    = model.Person{Name: "Ben Schaffer", Email: "ben@wsgr.com"}

	// The three reference customers' procurement leads. "Anything from them is
	// P1 and gets a same-day reply, period."
	Renee = model.Person{Name: "Renee Tan", Email: "renee@halberd.example"}
	Dana  = model.Person{Name: "Dana Whitfield", Email: "dana@northstar.example"}
	Luis  = model.Person{Name: "Luis Ferrer", Email: "luis@veritas.example"}

	// Hiring. Mei Tanaka's loop is already closed and she is named in the notes
	// rather than given a mailbox: a candidate who has signed writes no email
	// the digest has to reason about.
	Ravi = model.Person{Name: "Ravi Desai", Email: "ravi.desai@fastmail.example"}

	// Vendors, prospects, and the noise the profile names by category.
	Lumen        = model.Person{Name: "Cassie Mueller", Email: "cassie@lumen.example"}
	Ines         = model.Person{Name: "Ines Marchetti", Email: "ines@brightmoor.example"}
	Stratechery  = model.Person{Name: "Stratechery", Email: "newsletter@stratechery.example"}
	Pagerail     = model.Person{Name: "Pagerail", Email: "product@pagerail.example"}
	KestrelTalia = model.Person{Name: "Talia Brandt", Email: "talia@kestrelsearch.example"}
	KestrelOwen  = model.Person{Name: "Owen Vance", Email: "owen@kestrelsearch.example"}
	KestrelRao   = model.Person{Name: "Priyanka Rao", Email: "priyanka@kestrelsearch.example"}
)

// to is shorthand for a recipient list. Scenario scripts read as prose and this
// is the one piece of syntax that would otherwise fight that.
func to(people ...model.Person) []model.Person { return people }
