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
        Console.postText("/complete", body, ctx.socket, ctx.peer).then(said -> { text.accept(said); return null; });
    }

    @Override
    public void look(CompanionContext ctx, String path, String numbered, Consumer<String> notes) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("path", path);
        body.set("text", numbered);
        Console.postText("/look", body, ctx.socket, ctx.peer).then(said -> { notes.accept(said); return null; });
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
    public void draftCommitMessage(CompanionContext ctx, String rules, Consumer<String> said) {
        elemental2.dom.URLSearchParams body = new elemental2.dom.URLSearchParams();
        body.set("rules", rules == null ? "" : rules);
        Console.postText("/git-msg", body, ctx.socket, ctx.peer).then(w -> { said.accept(w); return null; });
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
