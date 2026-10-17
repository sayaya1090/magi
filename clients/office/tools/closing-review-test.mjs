import assert from 'node:assert/strict';
for (const product of ['word', 'excel', 'powerpoint']) {
  const { Transcript } = await import(`../../${product}/addin/src/domain/Transcript.js`);
  const { councilBody } = await import(`../../${product}/addin/src/ui/screen.js`);
  const data = { round:1, decision:'continue', tally:{done:3,continue:0,voters:3}, closingDecision:'continue', closingReason:'verify 72dp' };
  const transcript = new Transcript();
  transcript.append({seq:1,type:'council.decided',data});
  assert.match(councilBody(transcript.rows[0]), /최종 종합 검토: 보류\nverify 72dp/);
  const legacy = new Transcript();
  legacy.append({seq:1,type:'council.decided',data:{...data,closingDecision:undefined,feedback:'verify 72dp'}});
  assert.match(councilBody(legacy.rows[0]), /최종 결과 보류 \(이전 기록\)\nverify 72dp/);
  console.log(`PASS: ${product} closing review and legacy replay`);
}
