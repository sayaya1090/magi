package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

import java.util.List;
import java.util.function.Consumer;

/**
 * 워크스페이스가 세상에 대는 포트 — 새 엔드포인트는 없다: 운영 콘솔이 쓰던 그 셋이다.
 * 디렉토리는 한 번에 여러 개를 청한다(운영 fetchDirs): 열린 가지를 하나씩 물으면 걸음마다
 * 왕복이 붙는다.
 */
public interface WorkspaceSource {
    /** 디렉토리들의 내용 — 답은 {dirs:{경로:[{name,isDir}]}} 그대로다. */
    void dirs(CompanionContext ctx, List<String> paths, Consumer<Object> gotOrNull);

    /** 이 워크스페이스의 git — 저장소가 아니면 repo:false로 온다(그것도 답이다). */
    void git(CompanionContext ctx, Consumer<Object> gotOrNull);

    /** 파일 하나의 본문 — {path,text}. */
    void file(CompanionContext ctx, String path, Consumer<Object> gotOrNull);

    /**
     * 파일 본문을 쓴다(/save) — 패치를 만들 수 있으면 패치로, 아니면 본문으로.
     *
     * 패치가 기본인 이유는 크기가 아니라 <b>거절할 수 있다</b>는 것이다: 열어 둔 사이 컴패니언이
     * 그 파일을 고쳤으면 문맥이 안 맞아 git이 그렇다고 말하고, 남의 일을 지웠을 저장이 문장
     * 하나가 된다. 통째로 보내면 마지막에 쓴 사람이 이긴다.
     */
    void save(CompanionContext ctx, String path, String patch, String text, Consumer<String> why);

    /**
     * 파일에 하는 일(/file-do): new-file · new-dir · rename · delete.
     * why는 거부 사유이고 성공은 빈 문자열이다 — 무엇이 됐는지는 다시 걸어 확인한다.
     */
    void fileDo(CompanionContext ctx, String what, String path, String to, Consumer<String> why);

    /**
     * 찾기(/find) — 이름으로(in=name) 또는 내용으로(in=text). 답은 {hits:[…],more:n}.
     * 에이전트가 일하는 워크스페이스를 뒤지는 일이라 이 타입의 것이다.
     */
    void find(CompanionContext ctx, String in, String q, Consumer<Object> gotOrNull);

    /**
     * 캐럿 자리의 이어쓰기(/complete) — 앞과 뒤를 함께 보낸다(사람은 줄 끝에서만 쓰지 않는다).
     * 답은 이어붙일 글 그대로이고, 없으면 빈 문자열이다.
     */
    void complete(CompanionContext ctx, String path, String prefix, String suffix, Consumer<String> text);

    /**
     * 열어 둔 파일과 아직 디스크에 없는 그 내용(/open-file) — 컴패니언의 다음 턴이 그 편집에
     * 대해 답할 수 있게 한다. 빈 본문은 그 사본을 <b>지운다</b>(계약의 나머지 반).
     */
    void openFileHint(CompanionContext ctx, String path, String text);

    /**
     * 한 파일의 차이(/diff) — which는 무엇에 대한 차이인가: ""(아직 안 실은 것) · staged · untracked.
     * 답은 {text}이고, 빈 본문은 "다른 데가 없다"는 <b>답</b>이다(못 읽은 것과 다르다).
     */
    void diff(CompanionContext ctx, String path, String which, Consumer<Object> gotOrNull);

    /** git에 하는 일(/git-do): stage · unstage · discard · commit · switch · new-branch · pull · push. */
    void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why);
}
