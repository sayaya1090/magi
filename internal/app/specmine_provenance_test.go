package app

import (
	"strings"
	"testing"
)

// Two blocks of mined context reach every executor, and they do not have the same standing: the
// execution note is distilled from the request BEFORE anything in the workspace is opened, while the
// repository findings are read out of the real files. Nothing said so, and the labels ran the wrong
// way round — the note's ⟨hard⟩ lines read as "match verbatim" while the findings called themselves
// "not ground truth". Observed live: three ⟨hard⟩ lines predicting one transformation of a data file
// sat directly above a read-only pass's reading of the actual bytes describing a completely
// different one, and the prediction was the authoritative-looking half. What each block KNOWS has to
// be part of what it says.
func TestMinedContextStatesWhatItKnowsAndWhatItGuessed(t *testing.T) {
	note := specMineNote("- ⟨hard⟩ input.csv has CRLF line endings → :%s/\\r$//g")
	for _, want := range []string{
		"BEFORE any file in this workspace was opened",
		"is a prediction to verify, never a value to match",
		"a fixed identifier/value the REQUEST itself pins",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the execution note must state its own provenance %q:\n%s", want, note)
		}
	}
	// The distillation itself must still arrive untouched — provenance is a frame, not a filter.
	if !strings.Contains(note, `:%s/\r$//g`) {
		t.Errorf("the mined lines must survive verbatim:\n%s", note)
	}

	found := specMineFindingsNote("- input.csv:1 — `1\\t  alpha1  ,  BeTa1  `")
	if !strings.Contains(found, "this pass opened the file and that note did not") {
		t.Errorf("the findings must say why they outrank a prediction:\n%s", found)
	}
	// The demotion this note already carried is against the FILE, and must survive: a sample read
	// once can be misread, so an executor whose own read disagrees still trusts the file.
	if !strings.Contains(found, "TRUST THE FILE") {
		t.Errorf("the findings must keep their own caveat:\n%s", found)
	}
	if !strings.Contains(found, "alpha1") {
		t.Errorf("the findings must survive verbatim:\n%s", found)
	}
}

// The worker gets both blocks concatenated into one, under one header — and the tag legend lives in
// the note the PARENT session gets, so `⟨hard⟩` arrived at the worker with nothing anywhere in its
// window defining it. A tag no one defined still reads as authority.
func TestWorkerMinedBlockSeparatesPredictionFromReading(t *testing.T) {
	got := specMineWorkerBrief("- ⟨hard⟩ input.csv has CRLF line endings\n\n" +
		"# Repository findings — input.csv:1 — `1\\tGAMMA1;BETA1;ALPHA1;OK`")
	for _, want := range []string{
		"⟨hard⟩ = a value the request itself pins",
		"⟨unconstrained⟩ = the request does NOT pin this",
		"before any file here was opened",
		"is a prediction",
		"the pass that OPENED the file wins over the line that predicted it",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the worker's block must carry %q:\n%s", want, got)
		}
	}
	// Both sides still arrive verbatim: the block's whole value is the exact spelling.
	for _, want := range []string{"CRLF line endings", "GAMMA1;BETA1;ALPHA1;OK"} {
		if !strings.Contains(got, want) {
			t.Errorf("the mined content must survive verbatim:\n%s", got)
		}
	}
}
