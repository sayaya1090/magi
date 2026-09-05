/**
 * 통합 문서에 닿는 문 — 화면이 Excel 에 대해 아는 것은 이것뿐이다.
 *
 * 파워포인트 판의 DeckPort 와 같은 자리다. 「덱·슬라이드·도형」이 「통합 문서·시트·범위」로 바뀌었을 뿐
 * 계약은 같다: 어댑터가 던지면 던진 대로 올라가고(조용한 성공 금지), 화면은 이 문 너머를 모른다.
 */
export class WorkbookPort {
  /**
   * 지금 잡혀 있는 선택. 엑셀에는 늘 선택이 있다(활성 셀) — 그래서 「없음」은 빈 셀 하나다.
   * @returns {Promise<{sheet:string, sheetIndex:number|null, address:string, rowCount:number, columnCount:number,
   *   values:any[][], valuesTruncated:boolean, textUnavailable:boolean}>}
   */
  async selection() { throw new Error('not implemented'); }
  /** 그 시트의 그 범위를 고르고 보여 준다. 없는 시트·틀린 주소는 던진다. */
  async point(_sheet, _address) { throw new Error('not implemented'); }
  /** 시트 이름 → 1-based 탭 위치. 못 주는 호스트는 `null` — 지어내지 않는다. */
  async sheetNames() { return null; }
  get label() { return 'unknown'; }
  get isHost() { return false; }
  capabilities() {
    return { measured: false, note: '이 어댑터는 요구 집합을 재지 않는다', sets: [] };
  }
}
