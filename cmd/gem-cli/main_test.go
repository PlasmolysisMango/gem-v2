package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseLevelsIncludesIndependentNoble(t *testing.T) {
	got, err := parseLevels(" noble, hard, medium ")
	if err != nil || !reflect.DeepEqual(got, []string{"noble", "hard", "medium"}) {
		t.Fatalf("levels: %v %v", got, err)
	}
	got, err = parseLevels("easy,medium,hard,master")
	if err != nil || len(got) != 4 {
		t.Fatalf("original levels: %v %v", got, err)
	}
	if _, err := parseLevels("unknown"); err == nil || !strings.Contains(err.Error(), "noble") {
		t.Fatalf("invalid type: %v", err)
	}
}
