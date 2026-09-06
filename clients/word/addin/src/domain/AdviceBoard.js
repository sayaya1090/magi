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
export function foldAdvice(rows, { server = 'word' } = {}) {
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
    // **못 읽은 호출도 센다.** `itemsOf` 가 `null` 을 주는 것은 「빈 목록을 실었다」가 아니라
    // 「이 인자에서는 항목을 못 꺼낸다」다 — 모델이 `advise({slideId:'s1'})` 처럼 말을 빼고
    // 부른 판이 그렇다. 여기서 안 세면 그 호출은 포스트잇도 쪽지도 없이 **통째로 사라지고**,
    // 사람은 모델이 아무 말도 안 한 것으로 읽는다. 화면의 문장(「무엇을 말하는지 안 실려」)이
    // 그대로 맞는 경우라 세는 자리도 같다.
    if (list === null) { dropped += 1; continue; }
    list.forEach((it, i) => {
      // **말이 없으면 안 붙인다.** 붙이면 빈 포스트잇이 뜨고, 빈 포스트잇은 사람이 무엇을
      // 놓쳤는지 알 길을 안 준다. 대신 몇 장을 못 붙였는지 센다.
      const message = typeof it?.message === 'string' ? it.message.trim() : '';
      if (!message) { dropped += 1; return; }
      // **도구가 광고한 철자로 읽는다.** 스키마는 `slide_id`·`shape_ids` 라고 적어 놓고
      // 여기서는 `slideId`·`shapeIds` 만 봤다 — 모델은 광고된 대로 부르므로 **모든 안내가
      // 「어디를 가리키는지 안 실렸습니다」로 떴다.** 실물에서 그 화면을 봤다(2026-09-01):
      // 모델은 슬라이드와 도형을 정확히 짚어 보냈는데 포스트잇은 하나도 못 눌렸다.
      // 낙타등도 계속 받는다 — 목업의 픽스처가 그 철자를 쓰고, 둘을 받는 값이 0 이다.
      const paragraph = it.paragraph ?? it.para ?? null;
      items.push(new Advice({
        id: `${r.callId ?? r.seq}#${i}`,
        message,
        paragraph,
      }));
    });
  }
  return { items, strays: [...strays], dropped };
}

/**
 * 위 셈 중 **화면이 말해야 하는 둘**을 한 줄로 짓는다. 붙은 안내는 포스트잇이 스스로 말하므로
 * 여기 안 든다 — 여기 드는 것은 **안 붙은 것들의 사유**뿐이다.
 *
 * **글을 화면 밖에서 짓는다.** 화면에 두면 DOM 이 있어야 돌아서 못 재는데, 이 두 문장은 정확히
 * 「설정 한 줄이 기능을 껐다」와 「모델이 말을 빼고 불렀다」를 사람에게 알리는 유일한 통로다
 * (`pickNote` 를 화면 밖으로 내린 것과 같은 이유고, 거기서는 그 덕에 성공 갈래가 사유 그물에
 * 걸려 있던 것을 잡았다).
 *
 * 빈 문자열은 **할 말이 없다**는 뜻이고, 화면은 그때 쪽지 칸을 숨긴다.
 */
export function adviceNote({ strays = [], dropped = 0 } = {}) {
  const notes = [];
  // 이름이 우리 서버가 아니라서 못 붙인 것. **조용히 안 끝낸다** — 설정 한 줄이 기능을
  // 껐다는 사실이 화면 어딘가엔 있어야 한다.
  if (strays.length) {
    notes.push(`안내를 부른 도구가 이 창이 아는 이름이 아닙니다: ${strays.join(', ')}`);
  }
  if (dropped) notes.push(`안내 ${dropped}건은 무엇을 말하는지 안 실려 못 붙였습니다.`);
  // 둘 다면 한 줄에 나란히 선다. 뒤엣것을 덮어쓰면 남의 서버 이름이 화면에서 사라진다.
  return notes.join(' · ');
}

/** `mcp__ppt__advise` → `advise`. 서버 이름이 뭐든 뒤 토막은 도구 이름이다. */
function tail(name) {
  const at = name.lastIndexOf('__');
  return at < 0 ? name : name.slice(at + 2);
}

/**
 * 호출 인자에서 항목 목록. **한 장짜리 호출도 받는다** — 설계는 `items[]` 지만, 모델이 한 장을
 * 그냥 펴서 부른 것을 못 알아듣고 버리면 그 안내는 아무 데도 안 남는다.
 *
 * **`null` 과 `[]` 는 다른 답이다.** `[]` 는 「빈 목록을 실었다」고, `null` 은 「이 인자에서는
 * 항목을 못 꺼낸다」다. 둘을 같은 값으로 접으면 못 읽은 호출이 조용히 없던 일이 되는데, 그것이
 * 이 파일이 `strays` 를 두는 이유와 같은 결함이다(§5.7 — 부재의 사유를 값에 싣는다).
 */
function itemsOf(args) {
  if (args == null) return null;
  if (Array.isArray(args)) return args;
  // 빈 배열이면 **빈 목록이다** — 못 읽은 게 아니다.
  if (Array.isArray(args.items)) return args.items;
  if (typeof args.message === 'string') return [args];
  return null;
}

/** 글이면 그대로, 아니면 `null`. **없는 것과 다른 종류가 온 것을 같게 다룬다** — 둘 다 못 쓴다. */
function str(v) { return typeof v === 'string' && v !== '' ? v : null; }

/** 배열이면 글자만 걸러서, 아니면 `null`. 빈 배열은 **빈 배열이다** — 못 읽은 게 아니다. */
