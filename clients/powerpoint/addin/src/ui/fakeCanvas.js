// PowerPoint 자리를 대신하는 미니 캔버스. **목업 전용이고 애드인에는 안 들어간다.**
// 여기 있는 이유는 하나다 — 이 머신에 PowerPoint 가 없어서 선택을 만들 곳이 필요하다.
export function mountFakeCanvas(deck, root) {
  // **제 칸만 갈아 끼운다.** `root` 를 통째로 비우면 같은 `root` 에 나중에 붙은 칸이 첫
  // 선택에서 사라진다 — 실제로 물음·스트림 단추들이 그렇게 없어졌고, 그러면 §5.7의 물음
  // 창을 눌러 볼 방법이 화면에서 없어진다. 못 눌러 본 화면은 안 만든 화면이다.
  const host = document.createElement('div');
  root.append(host);
  const render = () => {
    host.replaceChildren();

    const strip = document.createElement('div');
    strip.className = 'strip';
    for (const s of deck.model.slides) {
      const b = document.createElement('button');
      b.className = 'thumb' + (s.id === deck.currentSlide ? ' on' : '');
      b.textContent = `${s.no}`;
      b.title = s.title;
      b.addEventListener('click', () => deck.goTo(s.id));
      strip.append(b);
    }

    const slide = deck.slide(deck.currentSlide);
    const canvas = document.createElement('div');
    canvas.className = 'canvas';
    for (const sh of slide.shapes) {
      const el = document.createElement('div');
      el.className = 'shape' + (deck.selected.has(sh.id) ? ' sel' : '');
      el.style.left = sh.x + '%';
      el.style.top = sh.y + '%';
      el.style.width = sh.w + '%';
      el.style.height = sh.h + '%';
      el.textContent = sh.text || `[${sh.type}]`;
      el.addEventListener('click', (e) => deck.click(sh.id, e.shiftKey));
      canvas.append(el);
    }

    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = '도형을 클릭해 잡고(Shift 로 여러 개) 오른쪽에서 「선택 인용」을 누릅니다. 진짜 PowerPoint 에서는 이 자리가 슬라이드 편집 영역입니다.';

    host.append(strip, canvas, hint);
  };
  deck.onChange(render);
  render();
}
