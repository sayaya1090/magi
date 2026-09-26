package app

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A file the task NAMES that did not exist when the turn started, and exists now.
//
// Measured live (2026-09-27, gpt-oss:120b-cloud): asked to count the rows of invoices.csv in a
// workspace that has none, the agent searched properly and said so. The council turned it away and
// told it to "add invoices.csv to the project and print its row count"; it wrote a three-row file of
// invented customers, ran `wc -l` on it, and all three members accepted "4" as "a real run matched".
// Every one of them had the write in front of them. The comparison that settles it — this file was
// not there when the task was given — is one only the turn's start can make, so it is made here and
// said as a fact, the way the pre-existing dirt is.
//
// It is a measurement, not a verdict: a task that asks for a file to be made names it too, and then
// its appearing is the work. The banner says which reading makes it evidence and which makes it the
// agent's own invention, and the member decides.

// fileNameRE matches the shape a task uses to name a file: an optional directory path and a name
// with an extension that starts with a letter ("invoices.csv", "src/app.py"). The letter keeps out
// version numbers and decimals; nothing else is filtered, because a false entry here only matters
// when a file of exactly that name appeared during the turn.
var fileNameRE = regexp.MustCompile(`(?:[\w.-]+/)*[\w-]+\.[A-Za-z][A-Za-z0-9]{0,7}\b`)

// namedFilesCreatedThisTurn returns the workspace paths that match a file the task names and were
// absent from base. With no baseline there is nothing to compare against, and it says nothing
// rather than calling every file new.
func namedFilesCreatedThisTurn(task, workdir string, base fileIndex) []string {
	if base == nil || strings.TrimSpace(task) == "" {
		return nil
	}
	named := map[string]bool{}
	for _, m := range fileNameRE.FindAllString(task, -1) {
		named[filepath.ToSlash(m)] = true
	}
	if len(named) == 0 {
		return nil
	}
	var out []string
	for p := range indexWorkspace(workdir) {
		if _, before := base[p]; before {
			continue
		}
		rel := filepath.ToSlash(p)
		for n := range named {
			if rel == n || strings.HasSuffix(rel, "/"+n) {
				out = append(out, rel)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// createdInputsBanner renders the fact beside the evidence the council reads.
func createdInputsBanner(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return "── NAMED BY THE TASK, CREATED BY THIS TURN ──\n" +
		"The task names these files, and they did NOT exist when the task was given — this turn made " +
		"them: " + strings.Join(clipEach(paths, 8), ", ") + ".\n" +
		"If the task asked for them to be made, that is the work. If the task treats them as something " +
		"already there — data to read, count, parse or use — their contents are the agent's own " +
		"invention: any result computed from them proves nothing about what the task asked, and a " +
		"requirement resting on them is UNSATISFIED."
}
