package embed

import "strings"

// Chunk sizes, in runes (not bytes): a Korean character is three UTF-8 bytes, and a byte budget
// would give Korean text a third of the context English gets for the same limit.
//
// The values follow the chunker in oh-my-openagent's memory-core (recall/chunks.ts): passages of
// at most 1,200 characters, a paragraph break preferred as the cut when it is more than 400
// characters past the start, and 160 characters of overlap so a sentence cut at one edge is whole
// in the neighbour. They are a starting point, not a measured optimum.
const (
	ChunkRunes     = 1200
	chunkMinBreak  = 400
	chunkOverlap   = 160
	paragraphBreak = "\n\n"
)

// Chunks splits text into overlapping passages for embedding.
//
// # Why chunk at all
//
// One vector per document averages everything the document says. A long skill whose last step is
// the exact answer to a question scores like its first paragraph, which is usually setup. Scoring a
// document by its best passage lets the part that matches decide, and the passage that won is the
// excerpt worth showing.
//
// Text at or under ChunkRunes comes back as one chunk, unchanged, so a short memory embeds exactly
// as it did before chunking existed and its cached vector is still a hit. Empty text gives no chunks.
func Chunks(text string) []string {
	r := []rune(text)
	if len(r) == 0 {
		return nil
	}
	if len(r) <= ChunkRunes {
		return []string{text}
	}
	var out []string
	for start := 0; start < len(r); {
		end := min(len(r), start+ChunkRunes)
		if end < len(r) {
			// The last paragraph break inside the window, if it leaves a passage long enough to be
			// worth a vector of its own.
			if i := strings.LastIndex(string(r[start:end]), paragraphBreak); i >= 0 {
				if cut := start + len([]rune(string(r[start:end])[:i])); cut-start > chunkMinBreak {
					end = cut
				}
			}
		}
		out = append(out, string(r[start:end]))
		if end == len(r) {
			break
		}
		// Always advance, even when the overlap would reach back past the start.
		start = max(start+1, end-chunkOverlap)
	}
	return out
}
