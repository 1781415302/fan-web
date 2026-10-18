package matching

import (
	"testing"
)

func TestDecideAcceptAndGap(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.98, Score: 0.99},
		{Subject: Subject{ID: 2}, NameScore: 0.50, Score: 0.50},
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 1 {
		t.Fatalf("got %+v", d)
	}
	if d.Candidates == nil || len(d.Candidates) == 0 {
		t.Fatal("Candidates must be non-nil non-empty when ranked non-empty")
	}

	// 分差不足
	ranked2 := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.98, Score: 0.95},
		{Subject: Subject{ID: 2}, NameScore: 0.90, Score: 0.94},
	}
	d2 := Decide(q, ranked2, true)
	if d2.Code != Review {
		t.Fatalf("gap too small should Review: %+v", d2)
	}
}

func TestDecideRetrievalIncomplete(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 1, Score: 1},
	}
	d := Decide(q, ranked, false)
	if d.Code != Review {
		t.Fatalf("incomplete retrieval must Review: %+v", d)
	}
}

func TestDecideNoCandidates(t *testing.T) {
	d := Decide(Query{}, nil, true)
	if d.Code != NoCandidates {
		t.Fatalf("%+v", d)
	}
	if d.Candidates == nil {
		t.Fatal("Candidates must be non-nil empty slice")
	}
}

func TestDecideExcludedSecondIgnoredForGap(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.98, Score: 0.95},
		{Subject: Subject{ID: 2}, NameScore: 0.90, Score: 0.94, Blockers: []string{"类型冲突"}},
		{Subject: Subject{ID: 3}, NameScore: 0.50, Score: 0.50},
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 1 {
		t.Fatalf("excluded second should not block gap: %+v", d)
	}
}

func TestDecideConfirmTargetTop3AndLowScore(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.75, Score: 0.80},
		{Subject: Subject{ID: 2}, NameScore: 0.72, Score: 0.78},
		{Subject: Subject{ID: 3}, NameScore: 0.71, Score: 0.77},
		{Subject: Subject{ID: 4}, NameScore: 0.70, Score: 0.76},
	}
	// 目标在 Top3、NameScore>=0.70、无 Blockers → 可确认（消解低分/分差）
	d := ConfirmTarget(q, ranked, 2)
	if d.Code != Accepted || d.WinnerID != 2 {
		t.Fatalf("confirm target 2: %+v", d)
	}
	// 不在 Top3
	d2 := ConfirmTarget(q, ranked, 4)
	if d2.Code == Accepted {
		t.Fatalf("target outside top3 must not accept: %+v", d2)
	}
	// 身份分过低
	ranked[0].NameScore = 0.50
	rankedLow := []Candidate{
		{Subject: Subject{ID: 9}, NameScore: 0.50, Score: 0.50},
	}
	d3 := ConfirmTarget(q, rankedLow, 9)
	if d3.Code == Accepted {
		t.Fatalf("NameScore<0.70 must not confirm: %+v", d3)
	}
}

func TestDecideConfirmTargetHardConflict(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.80, Score: 0.85, Blockers: []string{"季号冲突：查询2 vs 候选1"}},
	}
	d := ConfirmTarget(q, ranked, 1)
	if d.Code == Accepted {
		t.Fatalf("hard conflict must not confirm: %+v", d)
	}
}

func TestDecideConfirmTargetDoesNotOverrideAutoAccept(t *testing.T) {
	q := Query{HasFileTitle: true}
	ranked := []Candidate{
		{Subject: Subject{ID: 1}, NameScore: 0.99, Score: 0.99},
		{Subject: Subject{ID: 2}, NameScore: 0.80, Score: 0.80},
	}
	d := ConfirmTarget(q, ranked, 2)
	if d.Code != Accepted || d.WinnerID != 1 {
		t.Fatalf("must keep auto winner 1, got %+v", d)
	}
}
