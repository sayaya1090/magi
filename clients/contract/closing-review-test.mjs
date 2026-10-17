import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
for (const source of ['../figma/ui.html', '../browser/extension/panel.js']) {
  const text = readFileSync(new URL(source, import.meta.url), 'utf8');
  const start = text.indexOf('function render(e)');
  const end = text.indexOf('async function events(', start);
  assert.ok(start >= 0 && end > start, `${source}: actual event renderer exists`);
  const messages = [];
  const context = vm.createContext({append: text => messages.push(text)});
  vm.runInContext(text.slice(start, end), context);
  const data = {round:1,decision:'continue',tally:{done:3,continue:0,voters:3},closingDecision:'continue',closingReason:'verify 72dp'};
  context.render({type:'council.decided',data});
  assert.match(messages[0], /위원 표결: 3명 승인 \/ 0명 보류\n최종 종합 검토: 보류\nverify 72dp/);
  context.render({type:'council.decided',data:{...data,closingDecision:undefined,feedback:'verify 72dp'}});
  assert.match(messages[1], /최종 결과 보류 \(이전 기록\)/);
  console.log(`PASS: ${source} final review and legacy outcome`);
}
