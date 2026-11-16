package arena

import (
	"context"
	"testing"

	"gem/internal/ai"
)

func TestMatchParallel(t *testing.T) {
	newEasy := func() ai.Agent { return ai.NewEasy() }
	newMed := func() ai.Agent { return ai.NewMedium() }
	st, err := MatchParallel(context.Background(), newMed, newEasy, 2, 12, 5, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Games != 12 || st.AvgTurns < 10 || st.WinsA < 0 || st.WinsA > 12 {
		t.Fatalf("bad stats %+v", st)
	}
	seen := 0
	st, err = Match(context.Background(), newEasy, newEasy, 3, 3, 9, func(i int, s MatchStats) {
		if s.Games != i+1 {
			t.Errorf("progress %d with %d games", i, s.Games)
		}
		seen++
	})
	if err != nil || st.Games != 3 || seen != 3 {
		t.Fatalf("serial match: %v %+v seen=%d", err, st, seen)
	}
}
