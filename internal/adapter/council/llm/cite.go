package llm

// citeNoEvidence is what a member sends when its verdict rests on the report's substance rather
// than on anything it observed. Recorded rather than punished: a done that names nothing observed
// is a fact about that verdict, and one a reader should be able to see.
//
// magi used to CHECK the rest — look each cited fragment up in the record it had shown, re-ask
// once when it could not find it, and downgrade the member to abstain if it stood by grounds that
// were not there. The reasoning was sound and the measurement was not: across two full waves it
// produced two downgrades in thirty verdicts and caught nothing.
//
// Both downgrades were wrong, and in the same way. A member quoted real evidence in a form that is
// not one contiguous span — one cut the middle out and marked it with an ellipsis, the other
// quoted a command, an arrow, and the two lines that came back from it. Both had read the record;
// only the shape of the quotation was unusual. The second cost a vote in a council that then split
// 1-1 with one abstention.
//
// Two rounds of widening followed (elisions, then joiners) and each one only made the check cheap
// enough not to have caused the harm it had already caused. Nothing made it catch anything. A gate
// whose whole measured record is two false positives is not paying for the place it sits in, so it
// is gone rather than tuned further. The `cite` field stays: a member's stated grounds are worth
// having in front of a reader, and reading them costs nothing.
//
// What replaced it is not another gate. The identifiers a task names are measured against the
// work (literals.go) — a comparison of two things magi already has, handed over as evidence with
// the judgement left to the members.
const citeNoEvidence = "NO-EVIDENCE"
