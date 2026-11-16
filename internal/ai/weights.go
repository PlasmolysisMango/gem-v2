package ai

// Weights parameterize the static evaluation. Defaults encode common expert advice
// (BGA "Tips splendor"): efficiency, early cheap engine, targeting L2/L3 point cards,
// noble planning, gold as a resource, and denying opponents.
type Weights struct {
	Points      float64 // value of one victory point
	BonusEarly  float64 // value of a bonus at the start of the game
	BonusLate   float64 // value of a bonus when someone is close to 15
	BonusCap    float64 // bonuses beyond this count in one color are worth less
	Token       float64 // value of one useful colored token
	Gold        float64 // value of one gold token
	NobleClose  float64 // scaling for noble progress (fraction of 3 points)
	Target      float64 // value of reachable point cards (discounted by turns)
	TargetDecay float64 // per-turn discount of reachable targets
	ReserveDead float64 // penalty for a reserved card that is far from affordable
	Relative    float64 // weight of the strongest opponent vs. mean of opponents
	Scale       float64 // eval difference that maps to ~73% win probability
}

// DefaultWeights are hand-tuned with cmd/gem-arena.
var DefaultWeights = Weights{
	Points:      1.0,
	BonusEarly:  0.9,
	BonusLate:   0.15,
	BonusCap:    5,
	Token:       0.1,
	Gold:        0.22,
	NobleClose:  0.55,
	Target:      0.55,
	TargetDecay: 0.72,
	ReserveDead: 0.25,
	Relative:    0.7,
	Scale:       2.5,
}
