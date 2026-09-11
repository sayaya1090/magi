import assert from 'node:assert/strict';
for (const product of ['word', 'excel', 'powerpoint']) {
  const { Transcript } = await import(`../../${product}/addin/src/domain/Transcript.js`);
  const { councilBody } = await import(`../../${product}/addin/src/ui/screen.js`);
  const transcript = new Transcript();
  const verdict = { type: 'council.verdict', data: { round: 1, member: 'Melchior', decision: 'done', rationale: 'checked', thought: 'first thought' } };
  transcript.append({ ...verdict, seq: 0 });
  transcript.append({ ...verdict, seq: 2, data: { ...verdict.data, thought: 'final thought\nsecond line' } });
  assert.equal(transcript.rows.length, 1, `${product}: live and saved verdict must remain one row`);
  assert.equal(transcript.rows[0].council.thought, 'final thought\nsecond line');
  assert.match(councilBody(transcript.rows[0]), /생각 \(판정 아님\)\nfinal thought\nsecond line/);
  const replay = new Transcript();
  replay.append({ ...verdict, seq: 2, data: { ...verdict.data, thought: 'final thought\nsecond line' } });
  assert.equal(councilBody(replay.rows[0]), councilBody(transcript.rows[0]));
  console.log(`PASS: ${product} council thought live, replay and rendering`);
}
