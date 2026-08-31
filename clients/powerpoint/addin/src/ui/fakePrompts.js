// 데몬이 묻는 상황을 손으로 만드는 자리. **목업 전용이고 애드인에는 안 들어간다.**
//
// 있는 이유는 하나다 — 이 머신에 데몬 문이 없어서 물음을 만들 곳이 필요하다. 그리고 이 칸이
// 없으면 §5.7이 세운 물음 창은 **아무도 못 본 채로** 착지한다. 못 본 화면은 안 만든 화면이다.

const CASES = [
  {
    label: '권한 물음',
    ask: {
      id: 'call_perm_1', kind: 'permission', what: 'bash',
      args: { command: 'rm -rf build/ && make', cwd: '/Users/me/deck' },
      reason: '허용 규칙에 없는 명령입니다',
    },
  },
  {
    label: '질문 (근거 있음)',
    ask: {
      id: 'call_ask_1#1', kind: 'question',
      what: '표지의 부제를 어느 쪽으로 맞출까요?',
      options: ['왼쪽 정렬', '가운데 정렬'],
      report: [
        { key: 'tried', text: '2·5·9쪽 부제는 왼쪽, 표지만 가운데입니다.' },
        { key: 'costs', text: '왼쪽으로 맞추면 표지가 본문과 같아지고, 가운데로 두면 표지만 다릅니다.' },
        { key: 'leaning', text: '왼쪽으로 기웁니다 — 어긋난 쪽이 하나뿐이라.' },
      ],
      index: 1, total: 2,
    },
  },
  {
    // **인자가 안 실린 권한 물음.** 소켓의 `Args` 는 `omitempty` 라 진짜로 이렇게 온다. 화면이
    // 이때 인자 칸을 통째로 안 만들던 자리라, 여기에 이 칸이 없으면 그 갈래를 **아무도 못 본다**
    // — 이 파일이 있는 이유가 그것이다. 「permission: bash」만 놓고 누르라는 화면이 어떻게
    // 생겼는지는 눈으로 봐야 안다.
    label: '인자 없는 권한 물음',
    ask: {
      id: 'call_perm_2', kind: 'permission', what: 'bash',
      reason: '허용 규칙에 없는 명령입니다',
    },
  },
  {
    // 코어의 `Waiting.Event` 가 `default:` 로 권한 물음을 만들어 내는 그 자리(§5.7).
    label: '모르는 종류',
    ask: { id: 'call_x_1', kind: 'confirm', what: '무언가를 확인해 주십시오' },
  },
];

/**
 * 안내 목록의 **성한 줄과 성치 못한 줄을 한 화면에** 올린다.
 *
 * 앞의 `clear_advice` 가 있어야 눌러도 목록이 안 불어난다(§6.1 의 걷기). 세 줄인 이유는 각각
 * 사람이 할 일이 달라서다 — 따라가면 되는 것 / 그 슬라이드가 사라져 낡은 것 / 모델이 어딘지를
 * 아예 안 말한 것.
 */
function pushAdviceRows(stream) {
  const call = (name, args) => stream.push({ type: 'part.appended',
    data: { messageId: 'adv', part: { kind: 'tool-call', toolCall: {
      name: `mcp__ppt__${name}`, callId: `c-${name}-${Date.now()}`, args } } } });
  call('clear_advice', {});
  call('advise', { items: [
    { message: '이 상자가 넘칩니다', slideId: 's4f2a1', shapeIds: ['sh8c30'] },
    { message: '이 안내는 낡았습니다 — 그 슬라이드가 없습니다', slideId: 's-지워짐' },
    { message: '어딘지는 안 실렸습니다', shapeIds: ['sh8c30'] },
  ] });
}

export function mountFakePrompts(status, root, { stream, readTranscript, sessionId, deck } = {}) {
  const box = document.createElement('div');
  box.className = 'fake-prompts';

  const title = document.createElement('span');
  title.textContent = '데몬 흉내:';
  box.append(title);

  for (const c of CASES) {
    const b = document.createElement('button');
    b.textContent = c.label;
    b.addEventListener('click', () => status.ask(c.ask));
    box.append(b);
  }

  const clear = document.createElement('button');
  clear.textContent = '남이 답함';
  clear.title = '물음만 내려간다 — 이 창은 무엇으로 답했는지 모른다';
  clear.addEventListener('click', () => status.clear());
  box.append(clear);

  // **같은 물음을 보는 동안 뒤가 늘어나는 자리.** 이게 없으면 「모두 N개」 줄이 늘 처음 값으로만
  // 뜨고, 그 줄이 안 고쳐지는 결함을 아무도 못 본다. 물음의 신원(`id`)은 그대로 두는 것이
  // 요점이다 — 신원이 바뀌면 판을 다시 세우므로 저절로 고쳐져서 아무것도 안 재게 된다.
  const more = document.createElement('button');
  more.textContent = '뒤에 하나 더';
  more.title = '서 있는 물음은 그대로 두고 뒤에 쌓인 수만 늘린다';
  more.addEventListener('click', () => {
    const p = status.pending;
    if (!p) return;
    status.ask({ ...p, index: p.index ?? 1, total: (p.total ?? 1) + 1 });
  });
  box.append(more);

  const cut = document.createElement('button');
  cut.textContent = '문 끊기 / 잇기';
  cut.addEventListener('click', () => { status.reachable = !status.reachable; });
  box.append(cut);

  // **연결이 둘이라는 것을 손으로 겪게 하는 자리.** 요청 쪽은 멀쩡한데 전사 스트림만 죽는
  // 경우가 진짜로 있고(§5.7), 그때 화면이 어떻게 되는지는 눌러 봐야 안다.
  if (stream && readTranscript) {
    const drop = document.createElement('button');
    drop.textContent = '스트림 끊기';
    drop.addEventListener('click', () => stream.drop());
    const back = document.createElement('button');
    back.textContent = '스트림 잇기';
    back.addEventListener('click', () => readTranscript.attach(sessionId));
    box.append(drop, back);

    // 두 화면은 **프레임이 있어야만 뜬다.** 눌러 볼 자리가 없으면 CSS 한 줄이 틀려도 아무도
    // 모른 채 착지한다 — 목업이 있는 이유가 바로 그것이다.
    const unver = document.createElement('button');
    unver.textContent = '검증 못 한 끝';
    unver.title = '`TurnFinishedData.Unverified` — 「고쳤다」와 「고쳤다는데 아무도 못 봤다」';
    unver.addEventListener('click', () => stream.push({ type: 'turn.finished',
      data: { unverified: true, reason: '독립 실행으로 확인된 것이 없습니다' } }));

    const stray = document.createElement('button');
    stray.textContent = '남의 서버 안내';
    stray.title = 'MCP 서버 이름이 설정값이라, 다르면 포스트잇이 한 장도 안 붙는다';
    stray.addEventListener('click', () => stream.push({ type: 'part.appended',
      data: { messageId: 'stray', part: { kind: 'tool-call', toolCall: {
        name: 'mcp__powerpoint__advise', callId: 'c-stray',
        args: { items: [{ message: '이건 안 붙어야 한다', slideId: 's4f2a1' }] } } } } }));

    box.append(unver, stray);

    // 안내 목록의 네 가지 「가리킬 곳」 표시는 **덱의 답이 달라야** 다 나온다. 눌러 볼 자리가
    // 없으면 「번호 확인 중」과 「이 호스트는 번호를 못 줍니다」가 같은 회색 줄로 착지한다.
    const rows = document.createElement('button');
    rows.textContent = '안내 세 줄';
    rows.title = '따라갈 것 · 낡은 것 · 어딘지 안 실린 것';
    rows.addEventListener('click', () => pushAdviceRows(stream));
    box.append(rows);

    if (deck) {
      const nums = document.createElement('button');
      nums.textContent = '번호 못 주는 덱';
      nums.title = 'PowerPointApi 1.8 아래 — 번호표가 null 이라 목록이 id 로 적는다';
      nums.addEventListener('click', () => {
        deck.numbering = !deck.numbering;
        nums.textContent = deck.numbering ? '번호 못 주는 덱' : '번호 주는 덱';
        pushAdviceRows(stream);
      });
      box.append(nums);

      // 두 번째 왕복이 죽는 날(`OfficeDeck.selection` 의 catch). 「글 없음」으로 적히면 사람도
      // 모델도 빈 상자를 고치러 간다 — 그 화면이 실제로 어떻게 생겼는지는 눌러 봐야 안다.
      const text = document.createElement('button');
      text.textContent = '글 못 읽는 덱';
      text.title = '신원은 오고 텍스트만 못 온 선택 — 인용이 「글 없음」이라 적으면 안 된다';
      text.addEventListener('click', () => {
        deck.readText = !deck.readText;
        text.textContent = deck.readText ? '글 못 읽는 덱' : '글 읽는 덱';
      });
      box.append(text);

      // 첫 왕복이 죽는 날. 위의 「글 못 읽는 덱」은 반쪽이고 이건 통째라, 인용은 한 장도 안
      // 붙는다 — 그때 화면이 **아무 말도 안 하면** 사람은 자기가 안 골랐다고 읽는다.
      const read = document.createElement('button');
      read.textContent = '선택 못 읽는 덱';
      read.title = 'selection() 이 던지는 경우 — 단추가 조용히 죽으면 안 된다';
      read.addEventListener('click', () => {
        deck.reading = !deck.reading;
        read.textContent = deck.reading ? '선택 못 읽는 덱' : '선택 읽는 덱';
      });
      box.append(read);
    }
  }

  root.append(box);
}
