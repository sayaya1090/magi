/**
 * 아주 작은 ZIP 쓰개. `.pptx` 는 zip 이고, `insertSlidesFromBase64` 가 받는 것도 zip 이다.
 *
 * # 왜 필요한가
 *
 * 이 애드인은 **덱에 슬라이드를 넣을 수 있다** — `duplicate_slide` 와 `restore_slide` 가 이미
 * `insertSlidesFromBase64` 로 넣는다. 다만 지금까지 넣은 것은 **덱에서 뜬 것**뿐이었다
 * (`slide.exportAsBase64()`). 우리가 **지은** 것을 넣으려면 zip 을 쓸 줄 알아야 하고, 옆의
 * `zip.js` 는 읽기만 한다.
 *
 * 그게 열리면 1.8 의 객체 모델이 못 하는 것들(네이티브 차트가 대표다)이 「불가능」이 아니라
 * 「우리가 OOXML 을 지어 넣으면 되는 것」이 된다.
 *
 * # 왜 압축을 안 하나
 *
 * **저장(store, method 0)만 쓴다.** OPC 는 압축을 요구하지 않고, 우리가 지을 것은 XML 몇 KB 라
 * 압축해서 아끼는 바이트보다 deflate 를 직접 구현하는 위험이 크다 — 틀리면 PowerPoint 가
 * 「복구할 수 없는 파일」이라고 말하고, 그 말은 사람에게 자기 덱이 망가진 것처럼 들린다.
 *
 * # 무엇을 안 하나
 *
 * zip64 를 안 한다(4GB 짜리 슬라이드는 없다). 암호도, 다중 볼륨도, 디렉토리 항목도 안 만든다 —
 * OPC 는 경로가 든 파일 항목만으로 읽힌다.
 */

/** CRC-32 표. 한 번만 짓는다. */
const CRC = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xEDB88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

export function crc32(bytes) {
  let c = 0xFFFFFFFF;
  for (let i = 0; i < bytes.length; i += 1) c = CRC[(c ^ bytes[i]) & 0xFF] ^ (c >>> 8);
  return (c ^ 0xFFFFFFFF) >>> 0;
}

/** 글을 UTF-8 바이트로. 이름도 내용도 같은 인코딩이어야 한다. */
function utf8(s) {
  return new TextEncoder().encode(s);
}

/**
 * 파일 목록을 zip 한 덩이로.
 *
 * @param {{name: string, data: string|Uint8Array}[]} files 경로와 내용
 * @returns {Uint8Array}
 */
export function zipStore(files) {
  const parts = [];
  const central = [];
  let offset = 0;

  const put = (arr) => { parts.push(arr); offset += arr.length; };
  const u16 = (n) => new Uint8Array([n & 0xFF, (n >>> 8) & 0xFF]);
  const u32 = (n) => new Uint8Array([n & 0xFF, (n >>> 8) & 0xFF, (n >>> 16) & 0xFF, (n >>> 24) & 0xFF]);

  for (const f of files) {
    const name = utf8(f.name);
    const data = typeof f.data === 'string' ? utf8(f.data) : f.data;
    const sum = crc32(data);
    const at = offset;

    // 로컬 헤더. 시각은 0 으로 둔다 — **같은 입력이 같은 바이트를 내야** 시험이 값을 잴 수 있고,
    // 시각을 넣으면 그 하나 때문에 매번 달라진다. zip 은 0 을 받아 준다.
    put(u32(0x04034B50));
    put(u16(20));            // 필요한 판본 2.0
    put(u16(0x0800));        // 이름이 UTF-8 이라고 알린다
    put(u16(0));             // 저장(store)
    put(u16(0)); put(u16(0)); // 시각·날짜
    put(u32(sum));
    put(u32(data.length));   // 압축 후 = 압축 전
    put(u32(data.length));
    put(u16(name.length));
    put(u16(0));             // 여분 필드 없음
    put(name);
    put(data);

    central.push({ name, sum, size: data.length, at });
  }

  const dirAt = offset;
  for (const e of central) {
    put(u32(0x02014B50));
    put(u16(20)); put(u16(20));
    put(u16(0x0800));
    put(u16(0));
    put(u16(0)); put(u16(0));
    put(u32(e.sum));
    put(u32(e.size)); put(u32(e.size));
    put(u16(e.name.length));
    put(u16(0)); put(u16(0)); // 여분·주석
    put(u16(0));              // 디스크 번호
    put(u16(0));              // 내부 속성
    put(u32(0));              // 외부 속성
    put(u32(e.at));
    put(e.name);
  }
  const dirSize = offset - dirAt;

  put(u32(0x06054B50));
  put(u16(0)); put(u16(0));
  put(u16(central.length)); put(u16(central.length));
  put(u32(dirSize)); put(u32(dirAt));
  put(u16(0));              // 주석 없음

  const out = new Uint8Array(offset);
  let i = 0;
  for (const part of parts) { out.set(part, i); i += part.length; }
  return out;
}

/**
 * base64 로. `insertSlidesFromBase64` 가 받는 모양이다.
 *
 * 브라우저의 `btoa` 는 **바이트 하나가 글자 하나**여야 하므로 latin1 로 옮겨 넘긴다 — 그냥
 * `String.fromCharCode(...bytes)` 를 쓰면 인자가 수십만 개인 호출이 되어 터진다.
 */
export function toBase64(bytes) {
  let s = '';
  const step = 0x8000;
  for (let i = 0; i < bytes.length; i += step) {
    s += String.fromCharCode.apply(null, bytes.subarray(i, i + step));
  }
  if (typeof btoa === 'function') return btoa(s);
  // 시험(Node)에서는 Buffer 로.
  return Buffer.from(bytes).toString('base64');
}
