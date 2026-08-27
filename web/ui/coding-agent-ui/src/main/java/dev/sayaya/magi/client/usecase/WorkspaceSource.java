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

    /** git에 하는 일(/git-do): stage · unstage · discard · commit · switch · new-branch · pull · push. */
    void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why);
}
