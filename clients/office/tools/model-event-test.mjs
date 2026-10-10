import assert from 'node:assert/strict';
for (const product of ['powerpoint', 'word', 'excel']) {
  const { Transcript } = await import(`../../${product}/addin/src/domain/Transcript.js`);
  const log = new Transcript();
  log.append({ seq: 1, type: 'model.changed', data: { model: 'new-model' } });
  assert.equal(log.unknownNote, null);
  assert.equal(log.rows.length, 1);
  assert.equal(log.rows[0].kind, 'note');
  assert.equal(log.rows[0].text, '모델 변경: new-model');
  log.append({ seq: 2, type: 'future.unknown', data: {} });
  assert.match(log.unknownNote, /future.unknown/);
  console.log(`PASS ${product}: model change is displayed; unknown events remain reported`);
}
