package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Stylesheet;
import dev.sayaya.magi.client.usecase.ModuleLoader;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLScriptElement;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.HashSet;
import java.util.Set;

/**
 * ModuleLoader의 스크립트 태그 구현 — GWT 모듈은 <name>.nocache.js 하나로 들어온다.
 * 경로는 /ui/ 절대: 상대경로는 프록시(BFF)로 새 나간다(관통 때 배운 그 결함).
 */
@Singleton
public class ScriptModuleLoader implements ModuleLoader {
    private final Set<String> loaded = new HashSet<>();

    @Inject
    public ScriptModuleLoader() {}

    @Override
    public void ensure(String module, boolean styles) {
        if (!loaded.add(module)) return;
        HTMLScriptElement s = (HTMLScriptElement) DomGlobal.document.createElement("script");
        s.src = "/ui/" + module + "/" + module + ".nocache.js";
        DomGlobal.document.head.append(s);
        // 시트가 스크립트와 같은 자리에서 걸린다 — 화면이 그릴 때 이미 입혀져 있게.
        if (styles) Stylesheet.ensure(module);
    }
}
