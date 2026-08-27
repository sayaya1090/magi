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

    private static String base(CompanionContext ctx) {
        return "?d=" + Global.encodeURIComponent(ctx.socket)
                + (ctx.peer != null && !ctx.peer.isEmpty() ? "&p=" + Global.encodeURIComponent(ctx.peer) : "");
    }
}
