package engine

// NumCards is the total number of development cards in the base game.
const NumCards = 90

// NumNobles is the total number of noble tiles.
const NumNobles = 10

// cardTable rows: level, bonus, points, cost in order black, blue, green, red, white.
var cardTable = [NumCards][8]int8{
	// Level 1 - black bonus
	{1, 4, 0, 0, 1, 1, 1, 1},
	{1, 4, 0, 0, 2, 1, 1, 1},
	{1, 4, 0, 0, 2, 0, 1, 2},
	{1, 4, 0, 1, 0, 1, 3, 0},
	{1, 4, 0, 0, 0, 2, 1, 0},
	{1, 4, 0, 0, 0, 2, 0, 2},
	{1, 4, 0, 0, 0, 3, 0, 0},
	{1, 4, 1, 0, 4, 0, 0, 0},
	// Level 1 - blue bonus
	{1, 1, 0, 1, 0, 1, 1, 1},
	{1, 1, 0, 1, 0, 1, 2, 1},
	{1, 1, 0, 0, 0, 2, 2, 1},
	{1, 1, 0, 0, 1, 3, 1, 0},
	{1, 1, 0, 2, 0, 0, 0, 1},
	{1, 1, 0, 2, 0, 2, 0, 0},
	{1, 1, 0, 3, 0, 0, 0, 0},
	{1, 1, 1, 0, 0, 0, 4, 0},
	// Level 1 - white bonus
	{1, 0, 0, 1, 1, 1, 1, 0},
	{1, 0, 0, 1, 1, 2, 1, 0},
	{1, 0, 0, 1, 2, 2, 0, 0},
	{1, 0, 0, 1, 1, 0, 0, 3},
	{1, 0, 0, 1, 0, 0, 2, 0},
	{1, 0, 0, 2, 2, 0, 0, 0},
	{1, 0, 0, 0, 3, 0, 0, 0},
	{1, 0, 1, 0, 0, 4, 0, 0},
	// Level 1 - green bonus
	{1, 2, 0, 1, 1, 0, 1, 1},
	{1, 2, 0, 2, 1, 0, 1, 1},
	{1, 2, 0, 2, 1, 0, 2, 0},
	{1, 2, 0, 0, 3, 1, 0, 1},
	{1, 2, 0, 0, 1, 0, 0, 2},
	{1, 2, 0, 0, 2, 0, 2, 0},
	{1, 2, 0, 0, 0, 0, 3, 0},
	{1, 2, 1, 4, 0, 0, 0, 0},
	// Level 1 - red bonus
	{1, 3, 0, 1, 1, 1, 0, 1},
	{1, 3, 0, 1, 1, 1, 0, 2},
	{1, 3, 0, 2, 0, 1, 0, 2},
	{1, 3, 0, 3, 0, 0, 1, 1},
	{1, 3, 0, 0, 2, 1, 0, 0},
	{1, 3, 0, 0, 0, 0, 2, 2},
	{1, 3, 0, 0, 0, 0, 0, 3},
	{1, 3, 1, 0, 0, 0, 0, 4},
	// Level 2 - black bonus
	{2, 4, 1, 0, 2, 2, 0, 3},
	{2, 4, 1, 2, 0, 3, 0, 3},
	{2, 4, 2, 0, 1, 4, 2, 0},
	{2, 4, 2, 0, 0, 5, 3, 0},
	{2, 4, 2, 0, 0, 0, 0, 5},
	{2, 4, 3, 6, 0, 0, 0, 0},
	// Level 2 - blue bonus
	{2, 1, 1, 0, 2, 2, 3, 0},
	{2, 1, 1, 3, 2, 3, 0, 0},
	{2, 1, 2, 0, 3, 0, 0, 5},
	{2, 1, 2, 4, 0, 0, 1, 2},
	{2, 1, 2, 0, 5, 0, 0, 0},
	{2, 1, 3, 0, 6, 0, 0, 0},
	// Level 2 - white bonus
	{2, 0, 1, 2, 0, 3, 2, 0},
	{2, 0, 1, 0, 3, 0, 3, 2},
	{2, 0, 2, 2, 0, 1, 4, 0},
	{2, 0, 2, 3, 0, 0, 5, 0},
	{2, 0, 2, 0, 0, 0, 5, 0},
	{2, 0, 3, 0, 0, 0, 0, 6},
	// Level 2 - green bonus
	{2, 2, 1, 0, 0, 2, 3, 3},
	{2, 2, 1, 2, 3, 0, 0, 2},
	{2, 2, 2, 1, 2, 0, 0, 4},
	{2, 2, 2, 0, 5, 3, 0, 0},
	{2, 2, 2, 0, 0, 5, 0, 0},
	{2, 2, 3, 0, 0, 6, 0, 0},
	// Level 2 - red bonus
	{2, 3, 1, 3, 0, 0, 2, 2},
	{2, 3, 1, 3, 3, 0, 2, 0},
	{2, 3, 2, 0, 4, 2, 0, 1},
	{2, 3, 2, 5, 0, 0, 0, 3},
	{2, 3, 2, 5, 0, 0, 0, 0},
	{2, 3, 3, 0, 0, 0, 6, 0},
	// Level 3 - black bonus
	{3, 4, 3, 0, 3, 5, 3, 3},
	{3, 4, 4, 0, 0, 0, 7, 0},
	{3, 4, 4, 3, 0, 3, 6, 0},
	{3, 4, 5, 3, 0, 0, 7, 0},
	// Level 3 - blue bonus
	{3, 1, 3, 5, 0, 3, 3, 3},
	{3, 1, 4, 0, 0, 0, 0, 7},
	{3, 1, 4, 3, 3, 0, 0, 6},
	{3, 1, 5, 0, 3, 0, 0, 7},
	// Level 3 - white bonus
	{3, 0, 3, 3, 3, 3, 5, 0},
	{3, 0, 4, 7, 0, 0, 0, 0},
	{3, 0, 4, 6, 0, 0, 3, 3},
	{3, 0, 5, 7, 0, 0, 0, 3},
	// Level 3 - green bonus
	{3, 2, 3, 3, 3, 0, 3, 5},
	{3, 2, 4, 0, 7, 0, 0, 0},
	{3, 2, 4, 0, 6, 3, 0, 3},
	{3, 2, 5, 0, 7, 3, 0, 0},
	// Level 3 - red bonus
	{3, 3, 3, 3, 5, 3, 0, 3},
	{3, 3, 4, 0, 0, 7, 0, 0},
	{3, 3, 4, 0, 3, 6, 3, 0},
	{3, 3, 5, 0, 0, 7, 3, 0},
}

// nobleTable rows: requirement in order white, blue, green, red, black.
var nobleTable = [NumNobles]Cost{
	{0, 0, 4, 4, 0},
	{0, 0, 0, 4, 4},
	{0, 4, 4, 0, 0},
	{4, 4, 0, 0, 0},
	{4, 0, 0, 0, 4},
	{3, 3, 0, 0, 3},
	{3, 3, 3, 0, 0},
	{0, 3, 3, 3, 0},
	{0, 0, 3, 3, 3},
	{3, 0, 0, 3, 3},
}

// Cards is the immutable card catalog indexed by id.
var Cards [NumCards]Card

// Nobles is the immutable noble catalog indexed by id.
var Nobles [NumNobles]Noble

// levelCards lists card ids per level (index 0 = level 1).
var levelCards [3][]int16

func init() {
	for i, r := range cardTable {
		c := Card{ID: int16(i), Level: r[0], Bonus: Color(r[1]), Points: r[2]}
		// table order: black, blue, green, red, white -> Cost order: white, blue, green, red, black
		c.Cost = Cost{r[7], r[4], r[5], r[6], r[3]}
		Cards[i] = c
		levelCards[c.Level-1] = append(levelCards[c.Level-1], int16(i))
	}
	for i, req := range nobleTable {
		Nobles[i] = Noble{ID: int8(i), Points: 3, Req: req}
	}
}

// LevelCards returns the card ids of a level (1..3).
func LevelCards(level int) []int16 { return levelCards[level-1] }
