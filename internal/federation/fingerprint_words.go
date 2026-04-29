package federation

import "crypto/sha256"

// sha256First8 returns the first 8 bytes of SHA-256(input).
func sha256First8(input []byte) []byte {
	sum := sha256.Sum256(input)
	out := make([]byte, 8)
	copy(out, sum[:8])
	return out
}

// phoneticWords maps a byte (0-255) to a short pronounceable English
// word, similar in spirit to the PGP word list. The list is curated for
// pronounceability and unambiguity over a phone line — no homophones
// (dear/deer), no easily-confused pairs (sort/short).
//
// Source list is shorter than the PGP word list (256 vs 512) because we
// only use a single-byte index. This costs 1 bit of mnemonic entropy
// per word vs full PGP, which is acceptable: the underlying security
// is the SHA-256 truncation, not the encoding.
// phoneticWords is a slice (not a fixed-size array) so additions to the
// list don't break compilation. Indexing uses len(phoneticWords) modulo
// to map a byte (0-255) — distribution is uniform when len = 256, and
// degrades gracefully if someone trims or extends the list.
var phoneticWords = []string{
	"aardvark", "absurd", "accrue", "acme", "adrift", "adult", "afflict", "ahead",
	"aimless", "Algol", "allow", "alone", "ammo", "ancient", "apple", "artist",
	"assume", "Athens", "atlas", "Aztec", "baboon", "backfield", "backward", "banjo",
	"beaming", "bedlamp", "beehive", "beeswax", "befriend", "Belfast", "berserk", "billiard",
	"bison", "blackjack", "blockade", "blowtorch", "bluebird", "bombast", "bookshelf", "brackish",
	"breadline", "breakup", "brickyard", "briefcase", "Burbank", "button", "buzzard", "cement",
	"chairlift", "chatter", "checkup", "chisel", "choking", "chopper", "Christmas", "clamshell",
	"classic", "classroom", "cleanup", "clockwork", "cobra", "commence", "concert", "cowbell",
	"crackdown", "cranky", "crowfoot", "crucial", "crumpled", "crusade", "cubic", "dashboard",
	"deadbolt", "deckhand", "dogsled", "dragnet", "drainage", "dreadful", "drifter", "dropper",
	"drumbeat", "drunken", "Dupont", "dwelling", "eating", "edict", "egghead", "eightball",
	"endorse", "endow", "enlist", "erase", "escape", "exceed", "eyeglass", "eyetooth",
	"facial", "fallout", "flagpole", "flatfoot", "flytrap", "fracture", "framework", "freedom",
	"frighten", "gazelle", "Geiger", "glitter", "glucose", "goggles", "goldfish", "gremlin",
	"guidance", "hamlet", "highchair", "hockey", "indoors", "indulge", "inverse", "involve",
	"island", "jawbone", "keyboard", "kickoff", "kiwi", "klaxon", "locale", "lockup",
	"merit", "minnow", "miser", "Mohawk", "mural", "music", "necklace", "Neptune",
	"newborn", "nightbird", "Oakland", "obtuse", "offload", "optic", "orca", "payday",
	"peachy", "pheasant", "physique", "playhouse", "Pluto", "preclude", "prefer", "preshrunk",
	"printer", "prowler", "pupil", "puppy", "python", "quadrant", "quiver", "quota",
	"ragtime", "ratchet", "rebirth", "reform", "regain", "reindeer", "rematch", "repay",
	"retouch", "revenge", "reward", "rhythm", "ribcage", "ringbolt", "robust", "rocker",
	"ruffled", "sailboat", "sawdust", "scallion", "scenic", "scorecard", "Scotland", "seabird",
	"select", "sentence", "shadow", "shamrock", "showgirl", "skullcap", "skydive", "slingshot",
	"slowdown", "snapline", "snapshot", "snowcap", "snowslide", "solo", "southward", "soybean",
	"spaniel", "spearhead", "spellbind", "spheroid", "spigot", "spindle", "spyglass", "stagehand",
	"stagnate", "stairway", "standard", "stapler", "steamship", "sterling", "stockman", "stopwatch",
	"stormy", "sugar", "surmount", "suspense", "sweatband", "swelter", "tactics", "talon",
	"tapeworm", "tempest", "tiger", "tissue", "tonic", "topmost", "tracker", "transit",
	"trauma", "treadmill", "Trojan", "trouble", "tumor", "tunnel", "tycoon", "uncut",
	"unearth", "unwind", "uproot", "upset", "upshot", "vapor", "village", "virus",
	"Vulcan", "waffle", "wallaby", "wayside", "willow", "woodlark", "Zulu", "adroitness",
	"adviser", "aftermath", "aggregate", "alkali", "almighty", "amulet", "amusement", "antenna",
	"applicant", "Apollo", "armistice", "article", "asteroid", "Atlantic", "atmosphere", "autopsy",
	"Babylon", "backwater", "barbecue", "belowground", "bifocals", "bodyguard", "bookseller", "borderline",
	"bottomless", "Bradbury", "bravado", "Brazilian", "breakaway", "Burlington", "businessman", "butterfat",
}
