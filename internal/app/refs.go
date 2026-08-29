package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A prompt's attached references, rendered ONCE by the core and persisted with the prompt.
//
// The IDE's selection and the composer's paperclip used to travel as a path:lines convention
// spliced into the person's text. Structured refs keep the words the person's words, and make the
// excerpt the core's rendering: resolved inside the workspace (the same jail every read keeps),
// sliced to the named lines, capped, and appended to the prompt event — so the transcript shows
// exactly what the agent was shown, and a replay shows it again.

// refCap bounds one excerpt, and refsCap the lot: an attachment is context, not a file transfer,
// and a prompt that arrives with a megabyte riding it is somebody's window gone.
const (
	refCap  = 16 << 10
	refsCap = 64 << 10
)

// appendRefs renders c.Refs and appends them to c.Parts as one block. Nothing to render appends
// nothing; a ref that cannot be served (outside the workspace, unreadable) renders its refusal in
// place, because an attachment that silently vanished is the worse failure.
func (a *App) appendRefs(ctx context.Context, c *command.SubmitPrompt) {
	if len(c.Refs) == 0 {
		return
	}
	workdir := a.sessionInfo(ctx, c.SessionID).Workdir
	var b strings.Builder
	total := 0
	for _, r := range c.Refs {
		b.WriteString(renderRef(workdir, r, &total))
	}
	if b.Len() == 0 {
		return
	}
	c.Parts = append(c.Parts, session.Part{Kind: session.PartText,
		Text: "── ATTACHED BY THE USER ──\n" + b.String()})
}

// renderRef is one attachment: a header naming what was asked for, then the excerpt or the reason
// there is none.
func renderRef(workdir string, r command.FileRef, total *int) string {
	head := "\n## " + r.Path
	if r.Lines != "" {
		head += " (lines " + r.Lines + ")"
	}
	head += "\n"
	if *total >= refsCap {
		return head + "(not shown — the attachments before this one already fill the budget)\n"
	}
	abs, err := insideWorkdir(workdir, r.Path)
	if err != nil {
		// The workspace is the trust boundary, for attachments exactly as for reads.
		return head + "(not shown — " + err.Error() + ")\n"
	}
	raw, rerr := os.ReadFile(abs)
	if rerr != nil {
		return head + "(not shown — " + rerr.Error() + ")\n"
	}
	text := sliceLines(string(raw), r.Lines)
	if len(text) > refCap {
		text = text[:refCap] + "\n… (the rest of this attachment is not shown)"
	}
	*total += len(text)
	return head + text + "\n"
}

// sliceLines cuts "12-40" (or "12") out of content, 1-indexed and inclusive, the way every editor
// counts. An unparseable range falls back to the whole content — the person pointed at the file,
// and losing the file over a malformed range would drop the half that mattered.
func sliceLines(content, lines string) string {
	if strings.TrimSpace(lines) == "" {
		return content
	}
	from, to := lines, lines
	if i := strings.IndexByte(lines, '-'); i >= 0 {
		from, to = lines[:i], lines[i+1:]
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(from))
	hi, err2 := strconv.Atoi(strings.TrimSpace(to))
	if err1 != nil || err2 != nil || lo < 1 || hi < lo {
		return content
	}
	all := strings.Split(content, "\n")
	if lo > len(all) {
		return fmt.Sprintf("(the file has %d lines; %s names none of them)", len(all), lines)
	}
	if hi > len(all) {
		hi = len(all)
	}
	return strings.Join(all[lo-1:hi], "\n")
}
