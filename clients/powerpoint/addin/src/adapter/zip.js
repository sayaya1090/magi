/**
 * 아주 작은 ZIP 읽개. `.pptx` 는 zip 이고, 슬라이드 하나의 `exportAsBase64()` 도 zip 이다.
 *
 * # 왜 여기 있나
 *
 * `export_slide_ooxml` 이 **base64 를 통째로 뱉으면 안 된다**(clients/powerpoint/DESIGN.md §7).
 * magi 의 도구 결과는 64KB 에서 잘리는데, 그 자료형에서는 정직한 잘림이 안 듣는다 —
 * **base64 의 앞부분은 조각이 아니라 쓰레기다.** 화면에도 로그에도 자료가 온 것처럼 보이는데
 * 풀면 아무것도 아니다. 그래서 **좁히는 것은 우리 일이고**, 좁히려면 풀어야 한다.
 *
 * # 무엇을 안 하나
 *
 * 쓰기를 안 한다. 암호·zip64·다중 볼륨도 안 한다. `.pptx` 안에서 실제로 쓰이는 둘만 읽는다 —
 * 저장(0)과 deflate(8). 못 읽는 항목은 **건너뛰지 않고 이름과 사유를 남긴다**(§10.5 의
 * 「없는 게 아니라 못 읽는다고 말한다」).
 */

const EOCD_SIG = 0x06054b50;
const CEN_SIG = 0x02014b50;

/**
 * 중앙 디렉토리를 읽어 항목 목록을 만든다.
 * @param {Uint8Array} bytes
 * @returns {{entries: Array<{name:string, method:number, start:number, size:number, csize:number}>}}
 */
export function zipEntries(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  // EOCD 는 뒤에서 찾는다. 주석이 붙을 수 있어 22 바이트 고정이 아니다.
  let eocd = -1;
  for (let i = bytes.length - 22; i >= 0 && i >= bytes.length - 22 - 0xffff; i--) {
    if (view.getUint32(i, true) === EOCD_SIG) { eocd = i; break; }
  }
  if (eocd < 0) throw new Error('zip 이 아닙니다 — 끝 기록을 못 찾았습니다');

  const count = view.getUint16(eocd + 10, true);
  let at = view.getUint32(eocd + 16, true);
  const entries = [];
  for (let i = 0; i < count; i++) {
    if (view.getUint32(at, true) !== CEN_SIG) {
      throw new Error(`zip 의 ${i + 1} 번째 항목이 깨졌습니다`);
    }
    const method = view.getUint16(at + 10, true);
    const csize = view.getUint32(at + 20, true);
    const size = view.getUint32(at + 24, true);
    const nameLen = view.getUint16(at + 28, true);
    const extraLen = view.getUint16(at + 30, true);
    const commentLen = view.getUint16(at + 32, true);
    const local = view.getUint32(at + 42, true);
    const name = new TextDecoder().decode(bytes.subarray(at + 46, at + 46 + nameLen));
    entries.push({ name, method, csize, size, local });
    at += 46 + nameLen + extraLen + commentLen;
  }
  return { entries };
}

/**
 * 항목 하나를 풀어 글로 돌려준다.
 *
 * `DecompressionStream` 을 쓴다 — 브라우저와 Node 둘 다 갖고 있고, **의존성을 하나도 안 늘린다.**
 * 없는 환경이면 그렇게 말한다(조용히 빈 문자열을 주면 「빈 슬라이드」로 읽힌다).
 */
export async function zipRead(bytes, name) {
  const { entries } = zipEntries(bytes);
  const found = entries.find((e) => e.name === name);
  if (!found) {
    throw new Error(`이 zip 에 ${name} 이 없습니다 — 들어 있는 것: ${entries.map((e) => e.name).join(', ')}`);
  }
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  // 로컬 헤더의 이름·엑스트라 길이는 중앙 디렉토리와 다를 수 있다. 로컬 것을 읽는다.
  const nameLen = view.getUint16(found.local + 26, true);
  const extraLen = view.getUint16(found.local + 28, true);
  const start = found.local + 30 + nameLen + extraLen;
  const raw = bytes.subarray(start, start + found.csize);

  if (found.method === 0) return new TextDecoder().decode(raw);
  if (found.method !== 8) throw new Error(`${name} 은 이 읽개가 모르는 압축(${found.method})입니다`);
  if (typeof DecompressionStream === 'undefined') {
    throw new Error('이 환경에는 DecompressionStream 이 없어 zip 을 못 풉니다');
  }
  const stream = new Blob([raw]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
  return new Response(stream).text();
}

/** base64 → 바이트. 애드인이 받는 것이 base64 라서 여기 둔다. */
export function fromBase64(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
