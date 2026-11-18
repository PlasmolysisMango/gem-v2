package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestJSONLConfigResultsAndSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--a=easy", "--b=easy", "--players=2", "--games=2", "--seed=19", "--parallel=1", "--jsonl"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	games, summaries := 0, 0
	for scanner.Scan() {
		data := scanner.Bytes()
		if !json.Valid(data) {
			t.Fatalf("invalid JSONL: %s", data)
		}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		switch header.Type {
		case "game":
			var line gameLine
			if err := json.Unmarshal(data, &line); err != nil {
				t.Fatal(err)
			}
			if line.SchemaVersion != 1 || line.G != games || line.Seed != 19 || line.SeatA != games%2 || len(line.Labels) != 2 || line.Labels[0] != "easy" || line.Labels[1] != "easy" {
				t.Fatalf("assignment/config: %+v", line)
			}
			c := line.Config
			if c.A != "easy" || c.B != "easy" || c.Players != 2 || c.Games != 2 || c.BaseSeed != 19 || c.Parallel != 1 || c.HardMS != 300 || c.MasterMS != 1500 || c.Workers != 0 || c.EffectiveMasterWorkers != min(runtime.NumCPU(), 8) || c.HardWorkers != 1 || c.AgentRNG == "" || c.GoVersion == "" {
				t.Fatalf("incorrect defaults: %+v", c)
			}
			r := line.Result
			if r.Status != "completed" || r.Error != "" || r.Cancelled || r.Duration <= 0 || len(r.Points) != 2 || len(r.Nobles) != 2 || len(r.CardLevels) != 2 || len(r.TieCredit) != 2 || len(r.Seats) != 2 || len(r.Evidence) != r.Turns {
				t.Fatalf("incomplete result: %+v", r)
			}
			credit, primary, decisions := 0.0, 0, 0
			for seat, m := range r.Seats {
				credit += r.TieCredit[seat]
				primary += m.PrimaryTurns
				decisions += r.Moves[seat]
				if m.EarlyLevel1.Count > 3 || r.Nobles[seat] != len(r.NobleIDs[seat]) {
					t.Fatal("incorrect seat metrics")
				}
			}
			rawDecisions := 0
			for _, tr := range r.Evidence {
				if !tr.Completed || len(tr.Before) != 64 || len(tr.After) != 64 {
					t.Fatal("incomplete evidence")
				}
				rawDecisions += len(tr.Decisions)
			}
			if credit != 1 || primary != r.Turns || decisions != rawDecisions {
				t.Fatal("incorrect aggregation")
			}
			games++
		case "summary":
			var line summaryLine
			if err := json.Unmarshal(data, &line); err != nil {
				t.Fatal(err)
			}
			if games != 2 || line.SchemaVersion != 1 || line.Status != "completed" || line.Stats.Games != 2 || line.Stats.StartedGames != 2 || line.Stats.RequestedGames != 2 || line.Stats.CancelledGames != 0 || line.Stats.FailedGames != 0 || line.Duration <= 0 || line.WinRateA != line.Stats.WinRateA() {
				t.Fatalf("bad summary: %+v", line)
			}
			summaries++
		default:
			t.Fatalf("unexpected line type %q", header.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if games != 2 || summaries != 1 || !strings.Contains(stderr.String(), "game 2/2") {
		t.Fatalf("games=%d summary=%d progress=%s", games, summaries, stderr.String())
	}
}

func TestJSONLExplicitConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--a=easy", "--b=easy", "--games=1", "--jsonl", "--hard-ms=7", "--master-ms=11", "--workers=2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	var line gameLine
	if err := json.Unmarshal(bytes.Split(stdout.Bytes(), []byte("\n"))[0], &line); err != nil {
		t.Fatal(err)
	}
	if line.Config.HardMS != 7 || line.Config.MasterMS != 11 || line.Config.Workers != 2 || line.Config.EffectiveMasterWorkers != 2 {
		t.Fatalf("config: %+v", line.Config)
	}
}

func TestLegacyTextAndHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--a=easy", "--b=easy", "--games=1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	for _, want := range []string{"easy vs easy (2 players, 1 games, seats rotated)", "easy win rate:", "expected by chance 50.0%", "avg turns:", "avg think:", "elapsed:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("missing %q in %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "easy|medium|hard|master|noble") {
		t.Fatalf("help: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestMalformedCLIInput(t *testing.T) {
	cases := [][]string{
		{"--players=0"}, {"--players=1"}, {"--players=5"},
		{"--games=0"}, {"--games=-1"}, {"--parallel=0"}, {"--parallel=-1"},
		{"--hard-ms=0"}, {"--hard-ms=-1"}, {"--master-ms=0"}, {"--master-ms=-1"},
		{"--hard-ms=9223372036854775807"}, {"--master-ms=9223372036854775807"},
		{"--workers=-1"}, {"--tune=-1"}, {"--tune=1", "--jsonl"},
		{"--a=missing"}, {"--b=missing"}, {"--games=nan"}, {"extra"},
		{"--tune=1", "--players=0"}, {"--tune=1", "--games=0"}, {"--tune=1", "--parallel=0"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestCancelledJSONLSummaryAndText(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"--a=easy", "--b=easy", "--games=3", "--jsonl"}, &stdout, &stderr)
	var summary summaryLine
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if code != 1 || summary.Status != "cancelled" || summary.Stats.Games != 0 || summary.Stats.StartedGames != 0 || summary.WinRateA != 0 || summary.Error == "" || !strings.Contains(stderr.String(), "0/3 games completed") {
		t.Fatalf("summary: %+v exit=%d stderr=%s", summary, code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(ctx, []string{"--a=easy", "--b=easy", "--games=3"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "0/3 games completed") {
		t.Fatalf("cancelled text: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
