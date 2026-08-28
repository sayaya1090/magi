package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.KnowledgeSource;
import elemental2.dom.URLSearchParams;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** KnowledgeSource의 회선 — 운영 loadSkills/loadWiki/loadMCP가 쓰던 그 경로들. */
@Singleton
public class FetchKnowledgeSource implements KnowledgeSource {
    @Inject
    public FetchKnowledgeSource() {}

    @Override
    public void skills(Consumer<Object> cb) { Console.fetchList("/skills", cb::accept); }

    @Override
    public void wiki(Consumer<Object> cb) { Console.fetchList("/wiki", cb::accept); }

    @Override
    public void mcp(Consumer<Object> cb) { Console.fetchList("/mcp", cb::accept); }

    @Override
    public void forget(String name, String tier, String team, String socket, String peer, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("name", name);
        body.set("tier", tier);
        if (team != null && !team.isEmpty()) body.set("team", team);
        Console.post("/forget", body, "project".equals(tier) ? socket : null, peer,
                (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void remember(String text, String tier, String team, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("text", text);
        body.set("tier", tier);
        if (team != null && !team.isEmpty()) body.set("team", team);
        Console.post("/remember", body, null, null, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void saveServer(String socket, JsPropertyMap<String> fields, java.util.function.Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        fields.forEach(k -> {
            String v = fields.get(k);
            if (v != null && !v.trim().isEmpty()) body.set(k, v.trim());
        });
        if (socket == null || socket.isEmpty()) body.set("tier", "global");
        Console.post("/mcp", body, socket == null || socket.isEmpty() ? null : socket, null, (ok, w) -> why.accept(Console.why(ok, w)));
    }

    @Override
    public void console(java.util.function.Consumer<String> embedModel) {
        Console.fetchList("/console", parsed -> {
            if (parsed == null) { embedModel.accept(""); return; }
            Object v = Js.asPropertyMap(parsed).get("embedModel");
            embedModel.accept(v == null ? "" : String.valueOf(v));
        });
    }

    @Override
    public void removeServer(String name, String socket, Consumer<String> why) {
        URLSearchParams body = new URLSearchParams();
        body.set("name", name);
        body.set("delete", "1");
        if (socket == null || socket.isEmpty()) body.set("tier", "global");
        Console.post("/mcp", body, socket == null || socket.isEmpty() ? null : socket, null,
                (ok, w) -> why.accept(Console.why(ok, w)));
    }
}
