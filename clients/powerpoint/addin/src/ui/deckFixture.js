// 가짜 덱의 내용. 목업이 무엇을 보여 줄지가 여기 있다 — 코드가 아니라 데이터다.
export const fixture = {
  slides: [
    { id: 's4f2a1', no: 4, title: '3분기', shapes: [
      { id: 'sh8c30', name: '제목 1', type: 'TextBox', text: '3분기 매출 현황 및 향후 전망',
        x: 6, y: 8, w: 88, h: 18, width: 360, height: 60,
        // **제안 카드가 목업에도 뜨게 한다**(§6.20). 이 화면이 이 저장소가 UI 를 눈으로 재는
        // 유일한 자리라, 여기 없으면 그 기능은 그림 없이 산다.
        //
        // 값의 모양은 `Suggestion.encodeFix` 가 만드는 그대로다 — 손으로 적어 두면 둘이
        // 어긋나고, 어긋난 쪽은 늘 이 픽스처다.
        tags: {
          'MAGI.FIX.DEMO1': JSON.stringify({
            what: '제목이 길어 두 줄로 넘칩니다',
            why: '아래 본문을 밀어서 여백이 사라집니다',
            fix: { tool: 'set_text', args: { shape_id: 'sh8c30', text: '3분기 매출 현황' } },
          }),
        } },
      { id: 'sh8c31', name: '본문 2', type: 'TextBox', text: '전년 대비 12% 성장',
        x: 6, y: 32, w: 55, h: 12, width: 225, height: 40 },
      { id: 'sh8c32', name: '차트 3', type: 'Chart', text: '',
        x: 64, y: 32, w: 30, h: 46, width: 123, height: 155 },
    ] },
    { id: 's7', no: 7, title: '리스크',
      // 장에 붙은 제안. **손이 안 달린 것도 있다** — 읽히기만 하고 안 눌리는 카드가 어떻게
      // 보이는지도 이 화면이 보여 줘야 한다.
      tags: {
        'MAGI.FIX.DEMO2': JSON.stringify({
          what: '이 장에는 발표 노트가 없습니다',
          why: '발표할 때 무슨 말을 할지 적어 두면 편합니다',
          fix: { tool: 'set_notes', args: { text: '리스크 셋 중 환율이 가장 큽니다.' } },
        }),
        'MAGI.FIX.DEMO3': JSON.stringify({
          what: '색이 너무 많습니다 — 두 가지로 줄이는 게 좋겠습니다',
          why: '읽는 사람이 어디를 봐야 할지 모릅니다',
        }),
      },
      shapes: [
      { id: 'sh7t', name: '제목 1', type: 'TextBox', text: '4분기 리스크 요인 및 대응 방안 정리',
        x: 6, y: 8, w: 88, h: 18, width: 360, height: 60 },
      { id: 'sh7b', name: '표 2', type: 'Table', text: '',
        x: 6, y: 32, w: 88, h: 46, width: 360, height: 155 },
    ] },
  ],
};
