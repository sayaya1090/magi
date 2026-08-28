package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.WorkspaceSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.List;
import java.util.function.Consumer;

/** WorkspaceSource의 회선 — 운영 콘솔이 쓰던 /files, /git, /file 그대로. */
@Singleton
public class FetchWorkspaceSource implements WorkspaceSource {
    @Inject
    public FetchWorkspaceSource() {}

    @Override
    public void dirs(CompanionContext ctx, List<String> paths, Consumer<Object> cb) {
        StringBuilder q = new StringBuilder(base(ctx));
        for (String p : paths) q.append("&path=").append(Global.encodeURIComponent(p));
        Console.fetchList("/files" + q, cb::accept);
    }

    @Override
    public void git(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/git" + base(ctx), cb::accept);
    }

    @Override
    public void file(CompanionContext ctx, String path, Consumer<Object> cb) {
        Console.fetchList("/file" + base(ctx) + "&path=" + Global.encodeURIComponent(path), cb::accept);
    }

    @Override
    public void find(CompanionContext ctx, String in, String q, Consumer<Object> cb) {
        Console.fetchList("/find" + base(ctx) + "&in=" + Global.encodeURIComponent(in)
                + "&q=" + Global.encodeURIComponent(q), cb::accept);
    }

    @Override
    public void save(CompanionContext ctx, String path, String patch, String text, Consumer<String> why) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("path", path);
        if (patch != null && !patch.isEmpty()) body.set("patch", patch);
        else body.set("text", text);
        Console.post("/save", body, ctx.socket, ctx.peer).then(w -> { why.accept(w); return null; });
    }

    @Override
    public void fileDo(CompanionContext ctx, String what, String path, String to, Consumer<String> why) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("do", what);
        body.set("path", path);
        if (to != null && !to.isEmpty()) body.set("to", to);
        Console.post("/file-do", body, ctx.socket, ctx.peer).then(w -> { why.accept(w); return null; });
    }

    @Override
    public void complete(CompanionContext ctx, String path, String prefix, String suffix, Consumer<String> text) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("path", path);
        body.set("prefix", prefix);
        body.set("suffix", suffix);
        // 사유를 <b>일부러</b> 버린다 — 사람이 누른 것이 아니라 타이핑이 부른 도움이라, 거절도
        // 침묵으로 지나가는 것이 맞다(누를 단추도, 사유를 적을 줄도 없다).
        Console.postText("/complete", body, ctx.socket, ctx.peer, (ok, said) -> text.accept(ok ? said : ""));
    }

    @Override
    public void look(CompanionContext ctx, String path, String numbered, WorkspaceSource.Said notes) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("path", path);
        body.set("text", numbered);
        Console.postText("/look", body, ctx.socket, ctx.peer, notes::call);
    }

    @Override
    public void openFileHint(CompanionContext ctx, String path, String text) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("path", path);
        body.set("text", text);
        Console.post("/open-file", body, ctx.socket, ctx.peer);
    }

    @Override
    public void diff(CompanionContext ctx, String path, String which, Consumer<Object> cb) {
        Console.fetchList("/diff" + base(ctx) + "&path=" + Global.encodeURIComponent(path)
                + "&which=" + Global.encodeURIComponent(which == null ? "" : which), cb::accept);
    }

    @Override
    public void pullRequest(CompanionContext ctx, Consumer<Object> cb) {
        Console.fetchList("/pr" + base(ctx), cb::accept);
    }

    @Override
    public void draftPullRequest(CompanionContext ctx, String rules, WorkspaceSource.Said said) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("rules", rules == null ? "" : rules);
        Console.postText("/pr-msg", body, ctx.socket, ctx.peer, said::call);
    }

    @Override
    public void openPullRequest(CompanionContext ctx, String title, String text, WorkspaceSource.Said urlOrWhy) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("title", title);
        body.set("body", text);
        Console.postText("/git-pr", body, ctx.socket, ctx.peer, urlOrWhy::call);
    }

    @Override
    public void draftCommitMessage(CompanionContext ctx, String rules, WorkspaceSource.Said said) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("rules", rules == null ? "" : rules);
        Console.postText("/git-msg", body, ctx.socket, ctx.peer, said::call);
    }

    @Override
    public void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("do", what);
        if (path != null && !path.isEmpty()) body.set("path", path);
        if (message != null && !message.isEmpty()) body.set("message", message);
        Console.post("/git-do", body, ctx.socket, ctx.peer).then(w -> { why.accept(w); return null; });
    }

    private static String base(CompanionContext ctx) {
        return "?d=" + Global.encodeURIComponent(ctx.socket)
                + (ctx.peer != null && !ctx.peer.isEmpty() ? "&p=" + Global.encodeURIComponent(ctx.peer) : "");
    }
}
