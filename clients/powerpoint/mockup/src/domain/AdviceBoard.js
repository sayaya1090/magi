import { Advice } from './Advice.js';

/**
 * 로그의 도구 호출을 안내 목록으로 접는다. **안내는 모델의 말이 아니라 도구 호출이다**(§6.1).
 *
 * 예전엔 가짜 어댑터가 `{kind:'advise'}` 를 만들어 화면에 직접 밀었다. 문은 그런 걸 안 준다 —
 * 모델이 `mcp__ppt__advise` 를 부르고, 그 호출이 `part.appended` 의 `tool-call` 조각으로
 * 로그에 앉는다. 그러니 안내 층은 **로그에서 유도되는 값**이고 따로 쌓아 두는 상태가 아니다.
 * 따로 쌓아 두면 다시 붙은 창이 남의 안내를 못 보고, 걷힌 안내를 계속 붙여 둔다.
 *
 * # 이름 하나에 달려 있다
 *
 * 도구 이름은 `mcp__<서버이름>__<도구>` 다(`manager.go` 의 `namespacedToolName`). **서버 이름은
 * 설정값이다** — 사용자가 MCP 서버를 `powerpoint` 로 적어 두면 도구는 `mcp__powerpoint__advise`
 * 가 되고, `ppt` 만 보는 창은 포스트잇을 **한 장도 안 붙인다.** 설정 한 줄이 기능 하나를 조용히
 * 끄는 모양이라, 여기서는 조용히 안 끝낸다: 이름이 `__advise` 로 끝나는데 우리 서버가 아니면
 * `strays` 에 담아 화면이 그 사실을 적게 한다(§5.7 의 「부재의 사유를 값에 싣는다」).
 */
export function foldAdvice(rows, { server = 'ppt' } = {}) {
  const mine = `mcp__${server}__`;
  const items = [];
  const strays = new Set();
  let dropped = 0;

  for (const r of rows) {
    if (r.kind !== 'tool' || !r.tool) continue;
    const bare = tail(r.tool);
    if (bare !== 'advise' && bare !== 'clear_advice') continue;
    if (!r.tool.startsWith(mine)) { strays.add(r.tool); continue; }
    if (bare === 'clear_advice') {
      // **못 붙인 셈도 같이 걷는다.** 이 수는 걷힌 그 호출들에서 나온 것이라, 남겨 두면
      // 없어진 안내를 두고 "몇 건은 못 붙였다"고 적는 쪽지가 된다 — 게다가 화면은 목록과
      // 쪽지가 **둘 다** 비어야 안내 층을 숨기므로(`view.js`), 다 걷은 판이 쪽지 하나 때문에
      // 계속 서 있게 된다.
      //
      // `strays` 는 **안 걷는다.** 그건 안내가 아니라 **설정이 어긋났다는 사실**이고, 우리
      // `clear_advice` 가 남의 서버 판을 걷지도 못한다. 걷히는 것은 우리가 세운 것뿐이다.
      items.length = 0;
      dropped = 0;
      continue;
    }

    const list = itemsOf(r.args);
    list.forEach((it, i) => {
      // **말이 없으면 안 붙인다.** 붙이면 빈 포스트잇이 뜨고, 빈 포스트잇은 사람이 무엇을
      // 놓쳤는지 알 길을 안 준다. 대신 몇 장을 못 붙였는지 센다.
      const message = typeof it?.message === 'string' ? it.message.trim() : '';
      if (!message) { dropped += 1; return; }
      items.push(new Advice({
        id: `${r.callId ?? r.seq}#${i}`,
        message,
        slideId: typeof it.slideId === 'string' ? it.slideId : null,
        shapeIds: Array.isArray(it.shapeIds)
          ? it.shapeIds.filter((x) => typeof x === 'string') : [],
      }));
    });
  }
  return { items, strays: [...strays], dropped };
}

/** `mcp__ppt__advise` → `advise`. 서버 이름이 뭐든 뒤 토막은 도구 이름이다. */
function tail(name) {
  const at = name.lastIndexOf('__');
  return at < 0 ? name : name.slice(at + 2);
}

/**
 * 호출 인자에서 항목 목록. **한 장짜리 호출도 받는다** — 설계는 `items[]` 지만, 모델이 한 장을
 * 그냥 펴서 부른 것을 못 알아듣고 버리면 그 안내는 아무 데도 안 남는다.
 */
function itemsOf(args) {
  if (args == null) return [];
  if (Array.isArray(args)) return args;
  if (Array.isArray(args.items)) return args.items;
  if (typeof args.message === 'string') return [args];
  return [];
}
