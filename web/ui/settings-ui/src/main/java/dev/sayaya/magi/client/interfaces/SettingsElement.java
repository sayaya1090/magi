package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.client.domain.Prefs;
import dev.sayaya.magi.client.usecase.SettingsStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 환경설정 — 다이얼로그가 아니라 화면이다.
 *
 * 운영이 그렇게 옮긴 이유가 이 화면의 성질을 말한다: 같은 컨트롤이 <b>어디에 서 있느냐로
 * 다른 파일을 고친다</b>(플릿에서 열면 이 기계의 config, 컴패니언에서 열면 그 컴패니언의
 * 것). 다이얼로그는 그 숨은 축을 감췄고, 화면은 그것을 맨 위에 적는다.
 *
 * 저장 버튼이 없다 — 모든 칸이 <b>바뀌는 순간</b> 저장된다. 무엇이 어디에 저장되는지는
 * 스토어가 안다(브라우저의 것과 데몬이 읽는 것은 다른 곳에 산다).
 */
@Singleton
public class SettingsElement {
    private final SettingsStore store;
    private final Notifications notifications;
    private final HTMLElement root = el("div");
    private boolean wired = false;

    @Inject
    public SettingsElement(SettingsStore store, Notifications notifications) {
        this.store = store;
        this.notifications = notifications;
    }

    public void mount(HTMLElement frame) {
        root.id = "settings";
        frame.replaceChildren(root);
        if (!wired) {
            wired = true;
            store.subscribe(this::render);
        }
        render();
        store.read();
    }

    private void render() {
        root.replaceChildren();
        HTMLElement head = el("div");
        head.id = "prefsK";
        head.textContent = tr("nav.preferences");
        root.append(head, scope());
        HTMLElement form = el("div");
        form.id = "prefsForm";
        form.append(group("grpAppearance", tr("pref.grp.appearance")));
        form.append(themeRow());
        form.append(langRow());
        form.append(group("grpNotify", tr("pref.grp.notify")));
        form.append(notifyRow());
        // 모델을 쓰는 것들은 그 능력이 있을 때만 — 서버가 어차피 거부하지만, 눌러서 거절에
        // 닿는 컨트롤은 없는 컨트롤보다 나쁘다(운영 data-may와 같은 판단).
        if (May.can("prompt")) {
            form.append(group("grpAssist", tr("pref.grp.assist")));
            form.append(switchRow("lookK", "lookWhy", "files.look", "files.look_why",
                    "lookover", true, on -> store.keep("lookover", Prefs.word(on))));
            form.append(switchRow("acK", "acWhy", "pref.autocomplete", "pref.autocomplete_why",
                    "autocomplete", true, on -> store.keep("autocomplete", Prefs.word(on))));
            form.append(switchRow("sugK", "sugWhy", "pref.suggest", "pref.suggest_why",
                    "suggest", true, on -> store.keep("suggest", Prefs.word(on))));
        }
        // 데몬이 읽는 것들 — config를 고치는 일이라 그 능력이 있어야 한다.
        if (May.can("configure")) for (HTMLElement one : completeGroup()) form.append(one);
        if (May.can("configure")) for (HTMLElement one : profilesGroup()) form.append(one);
        for (HTMLElement one : consoleGroup()) form.append(one);
        root.append(form);
    }

    /** 어느 파일을 고치는가 — 다음에 묻는 질문이고, 가서 읽을 수 있는 것이라 경로째 적는다. */
    private HTMLElement scope() {
        HTMLElement box = el("div");
        box.id = "settingsScope";
        box.className = "prefsay";
        HTMLElement k = el("div");
        k.className = "k";
        k.id = "settingsScopeK";
        String socket = store.socket();
        k.textContent = socket.isEmpty() ? tr("settings.scope_global")
                : tr("settings.scope_project", "name", nameOf(socket));
        HTMLElement say = el("div");
        say.className = "say";
        say.id = "settingsScopeFile";
        String file = str(complete(), "file");
        say.textContent = file.isEmpty() ? "" : tr("settings.scope_file", "file", file);
        box.append(k, say);
        return box;
    }

    private HTMLElement themeRow() {
        String now = store.pref("theme", "system");
        // 그림만으로는 "지금 무엇인가"를 말할 수 없다 — 운영도 그 줄에 지금의 테마를 적는다.
        HTMLElement row = row("themeK", "themeWhy", "pref.theme", "pref.theme." + now);
        HTMLElement btn = el("md-icon-button");
        btn.id = "themeToggle";
        btn.setAttribute("type", "button");
        // 무엇에 관한 버튼인지와 지금 무엇인지를 함께 — 그림(하늘)은 지금 것만 말할 수 있다.
        String said = tr("pref.theme") + ": " + tr("pref.theme." + now);
        btn.setAttribute("aria-label", said);
        btn.setAttribute("title", said);
        btn.textContent = "system".equals(now) ? "◐" : "light".equals(now) ? "☀" : "☾";
        btn.addEventListener("click", evt -> {
            String next = Prefs.nextTheme(store.pref("theme", "system"));
            store.keep("theme", next);
            applyTheme(next);
            render();
        });
        row.append(btn);
        return row;
    }

    private HTMLElement langRow() {
        HTMLElement row = row("langK", null, "pref.lang", null);
        HTMLElement sel = el("md-outlined-select");
        sel.id = "lang";
        String now = store.pref("lang", "system");
        for (String[] o : new String[][]{{"system", "pref.lang.system"}, {"en", "pref.lang.en"},
                {"ko", "pref.lang.ko"}}) {
            HTMLElement opt = el("md-select-option");
            opt.setAttribute("value", o[0]);
            HTMLElement head = el("div");
            head.setAttribute("slot", "headline");
            head.textContent = tr(o[1]);
            opt.append(head);
            if (o[0].equals(now)) opt.setAttribute("selected", "");
            sel.append(opt);
        }
        Js.asPropertyMap(sel).set("value", now);
        sel.addEventListener("change", evt -> {
            String want = value(sel);
            store.keep("lang", want);
            // 말이 바뀌면 이 창의 모든 화면이 제 말을 다시 칠한다 — 팩은 창에 하나다.
            Labels.reload(this::render);
        });
        put(row, sel);
        return row;
    }

    /**
     * 알림 — 이 브라우저가 구독하면, 페이지를 닫아 두어도 컴패니언이 기다릴 때 깨운다.
     *
     * 켤 수 없는 자리가 여럿이고 그 이유가 서로 다르다: https가 아니거나, 브라우저가 푸시를
     * 모르거나, 전에 거부했거나, 이 콘솔에 키가 없거나(데모가 그렇다). 스위치만 흐려 두면
     * 무엇이 문제인지 알 수 없으니 그 이유를 아래 줄에 적는다.
     */
    private HTMLElement notifyRow() {
        HTMLElement r = row("notifyK", "notifyWhy", "notify.k", "notify.how");
        HTMLElement sw = el("md-switch");
        sw.id = "notifySwitch";
        sw.setAttribute("touch-target", "wrapper");
        elemental2.dom.Element why = r.querySelector(".say");
        String blocked = notifications.blocked();
        if (!blocked.isEmpty()) {
            sw.setAttribute("disabled", "");
            if (why != null) why.textContent = tr(blocked);
        } else {
            notifications.subscribed(on -> {
                Js.asPropertyMap(sw).set("selected", on);
                if (why != null && on) why.textContent = tr("notify.is_on");
            });
            sw.addEventListener("change", evt -> {
                boolean want = Js.isTruthy(Js.asPropertyMap(sw).get("selected"));
                if (!want) {
                    notifications.turnOff((err, endpoint, p256dh, auth) -> {
                        if (!endpoint.isEmpty()) store.push(endpoint, p256dh, auth, true, () -> { });
                        if (why != null) why.textContent = err.isEmpty() ? tr("notify.how") : err;
                    });
                    return;
                }
                store.pushKey(key -> {
                    if (key == null || key.isEmpty() || "null".equals(key)) {
                        Js.asPropertyMap(sw).set("selected", false);
                        if (why != null) why.textContent = tr(store.demo() ? "notify.demo" : "notify.nokey");
                        return;
                    }
                    notifications.turnOn(key, (err, endpoint, p256dh, auth) -> {
                        if (!err.isEmpty() || endpoint.isEmpty()) {
                            Js.asPropertyMap(sw).set("selected", false);
                            if (why != null) why.textContent = err.startsWith("notify.") ? tr(err) : err;
                            return;
                        }
                        store.push(endpoint, p256dh, auth, false,
                                () -> { if (why != null) why.textContent = tr("notify.is_on"); });
                    });
                });
            });
        }
        put(r, sw);
        return r;
    }

    /** 데몬이 읽는 완성 설정 — 못 읽었으면 그 무리 자체를 그리지 않는다(빈 껍데기 금지). */
    private List<HTMLElement> completeGroup() {
        List<HTMLElement> out = new ArrayList<>();
        JsPropertyMap<Object> got = complete();
        if (got == null) return out;
        add(out, group("grpComplete", tr("ac.head")));
        HTMLElement why = el("div");
        why.className = "prefsay";
        why.id = "acsBlock";
        HTMLElement line = el("div");
        line.className = "say";
        line.id = "acsWhy";
        line.textContent = tr("ac.head_why");
        why.append(line);
        add(out, why);
        // 없는 키는 켜짐이 기본이다(운영: absent/true = default on).
        add(out, daemonSwitch("ambientK", "ambientWhy", "ac.ambient", "ac.ambient_why",
                !Js.isTruthy(got.get("ambient")) && got.has("ambient") ? false : true, "ambient"));
        add(out, daemonSwitch("crossK", "crossWhy", "ac.cross", "ac.cross_why",
                !Js.isTruthy(got.get("crossSession")) && got.has("crossSession") ? false : true, "crossSession"));
        add(out, profileRow("codeProfK", "codeProfWhy", "ac.code_profile", "ac.code_profile_why",
                "codeProfile", str(got, "codeProfile")));
        add(out, profileRow("compProfK", "compProfWhy", "ac.composer_profile", "ac.composer_profile_why",
                "composerProfile", str(got, "composerProfile")));
        // 커밋과 PR의 규칙 — 이 워크스페이스에서 쓰는 말투를 적어 두면 그대로 따른다.
        // 누를 때마다가 아니라 <b>손을 뗄 때</b> 저장한다: 글은 한 글자마다 끝나지 않는다.
        add(out, templateRow("commitTplK", "ac.commit_tpl", "commitTemplate", str(got, "commitTemplate")));
        add(out, templateRow("prTplK", "ac.pr_tpl", "prTemplate", str(got, "prTemplate")));
        return out;
    }

    /**
     * 모델 프로파일 — 위의 완성 설정이 <b>고르는 것</b>들이다.
     *
     * 목록과 폼이 한 자리에 있는 이유: 고치기는 그 줄을 폼에 실어 오는 일이고, 새로 만들기는
     * 빈 폼에 적는 일이다. 두 화면으로 가르면 "이건 새 것인가 고치는 것인가"를 사람이 기억해야
     * 한다. 키는 적었을 때만 보낸다 — 빈 칸은 "지우라"가 아니라 "그대로 두라"이다.
     */
    private List<HTMLElement> profilesGroup() {
        List<HTMLElement> out = new ArrayList<>();
        add(out, group("grpProfiles", tr("prof.head")));
        HTMLElement why = el("div");
        why.className = "prefsay";
        HTMLElement line = el("div");
        line.className = "say";
        line.id = "profWhy";
        line.textContent = tr("prof.head_why");
        why.append(line);
        add(out, why);

        HTMLElement listBox = el("div");
        listBox.className = "prefsay";
        HTMLElement list = el("div");
        list.className = "proflist";
        list.id = "profList";
        listBox.append(list);
        add(out, listBox);

        HTMLElement name = field("profName", tr("prof.name"));
        HTMLElement base = field("profBase", tr("prof.base_url"));
        HTMLElement model = field("profModel", tr("prof.model"));
        HTMLElement key = field("profKey", tr("prof.api_key"));
        key.setAttribute("supporting-text", tr("prof.api_key_hint"));
        HTMLElement save = el("md-text-button");
        save.id = "profSave";
        save.textContent = tr("prof.add");
        save.addEventListener("click", evt -> {
            String said = value(name).trim();
            if (said.isEmpty()) { line.textContent = tr("prof.need_name"); return; }
            store.saveProfile(said, value(base), value(model), value(key), false, w -> {
                if (w != null && !w.isEmpty()) { line.textContent = w; return; }
                line.textContent = tr("prof.head_why");
                Js.asPropertyMap(name).set("value", "");
                Js.asPropertyMap(base).set("value", "");
                Js.asPropertyMap(model).set("value", "");
                Js.asPropertyMap(key).set("value", "");
            });
        });
        // 지금 돌고 있는 백엔드에서 고른다 — 콘솔의 설정에서 뽑으면 그 데몬이 닿지도 못하는
        // 모델을 내놓는다. 고르면 <b>채우기만</b> 한다: 이름은 사람이 짓고 저장도 사람이 누른다.
        HTMLElement provRow = el("div");
        provRow.className = "profadd provrow";
        provRow.setAttribute("hidden", "");
        HTMLElement provSel = el("md-outlined-select");
        provSel.id = "provSel";
        provSel.setAttribute("label", tr("prof.provider"));
        HTMLElement provModel = el("md-outlined-select");
        provModel.id = "provModelSel";
        provModel.setAttribute("label", tr("prof.provider_model"));
        provRow.append(provSel, provModel);
        add(out, provRow);
        store.providers(got -> {
            JsArrayLike<Object> all = Js.uncheckedCast(got);
            // 서빙하는 것이 하나도 없으면 그 줄은 아예 서지 않는다 — 빈 고르개는 사람들에게
            // "여기는 열어 볼 것 없다"를 가르친다.
            if (all == null || all.getLength() == 0) return;
            provRow.removeAttribute("hidden");
            fill(provSel, names(all), tr("prof.provider"));
            fill(provModel, new java.util.ArrayList<>(), tr("prof.provider_model"));
            provSel.addEventListener("change", evt -> {
                JsPropertyMap<Object> chosen = byName(all, value(provSel));
                fill(provModel, models(chosen), tr("prof.provider_model"));
            });
            provModel.addEventListener("change", evt -> {
                JsPropertyMap<Object> chosen = byName(all, value(provSel));
                String pick = value(provModel);
                if (chosen == null || pick.isEmpty()) return;
                Js.asPropertyMap(base).set("value", str(chosen, "base"));
                Js.asPropertyMap(model).set("value", pick);
                if (value(name).trim().isEmpty()) Js.asPropertyMap(name).set("value", str(chosen, "name"));
            });
        });
        HTMLElement form = el("div");
        form.className = "profadd";
        form.append(name, base, model, key, save);
        add(out, form);

        JsArrayLike<Object> got = Js.uncheckedCast(store.profiles());
        for (int i = 0; got != null && i < got.getLength(); i++) {
            JsPropertyMap<Object> pr = Js.uncheckedCast(got.getAt(i));
            list.append(profileRowOf(pr, name, base, model));
        }
        if (got == null || got.getLength() == 0) {
            HTMLElement none = el("div");
            none.className = "profempty";
            none.textContent = tr("prof.none");
            list.append(none);
        }
        return out;
    }

    /** 한 줄 — 이름, 그것이 무엇인지, 그리고 고치기와 제거. */
    private HTMLElement profileRowOf(JsPropertyMap<Object> pr, HTMLElement name,
                                     HTMLElement base, HTMLElement model) {
        HTMLElement r = el("div");
        r.className = "profrow";
        HTMLElement nm = el("div");
        nm.className = "profnm";
        nm.textContent = str(pr, "name");
        HTMLElement meta = el("div");
        meta.className = "profmeta";
        String mdl = str(pr, "model");
        StringBuilder bits = new StringBuilder(mdl.isEmpty() ? tr("prof.no_model") : mdl);
        if (Js.isTruthy(pr.get("hasKey"))) bits.append("  ·  ").append(tr("prof.keyed"));
        bits.append("  ·  ").append("project".equals(str(pr, "tier"))
                ? str(pr, "companion") : tr("cron.machine"));
        meta.textContent = bits.toString();
        HTMLElement edit = el("md-text-button");
        edit.className = "profedit";
        edit.textContent = tr("action.edit");
        edit.addEventListener("click", evt -> {
            // 고치기는 그 줄을 폼으로 실어 오는 일이다 — 키는 실어 오지 않는다(보이지 않는 값이라).
            Js.asPropertyMap(name).set("value", str(pr, "name"));
            Js.asPropertyMap(base).set("value", str(pr, "baseUrl"));
            Js.asPropertyMap(model).set("value", str(pr, "model"));
            Js.<HTMLElement>uncheckedCast(name).focus();
        });
        HTMLElement drop = el("md-text-button");
        drop.className = "profdrop";
        drop.textContent = tr("action.remove");
        drop.addEventListener("click", evt ->
                store.saveProfile(str(pr, "name"), null, null, null, true, w -> { }));
        r.append(nm, meta, edit, drop);
        return r;
    }

    /** 이 콘솔 — 좁은 화면에서 접근 제어로 가는 길(레일이 접힌 자리의 그 문). */
    private List<HTMLElement> consoleGroup() {
        List<HTMLElement> out = new ArrayList<>();
        add(out, group("grpConsole", tr("pref.grp.console")));
        // 이 콘솔이 무엇인가 — 호스트, 어느 config를 읽고 있나, 그리고 판본 둘. 물어볼 데가
        // 여기뿐이라 적는다(창 전체의 사실이므로 셸이 읽어 올린 것을 든다).
        HTMLElement facts = el("div");
        facts.className = "prefsay";
        HTMLElement k = el("div");
        k.className = "k";
        k.id = "consoleK";
        k.textContent = tr("nav.this_console");
        HTMLElement lines = el("div");
        lines.id = "console";
        facts.append(k, lines);
        add(out, facts);
        dev.sayaya.magi.bridge.Facts.onConsole(info -> {
            if (info == null) return;
            JsPropertyMap<Object> c = Js.uncheckedCast(info);
            lines.replaceChildren();
            for (String[] one : new String[][]{{"field.host", str(c, "host")},
                    {"field.config", str(c, "configDir")},
                    {"field.console_version", str(c, "version")},
                    {"field.daemon_version", joinDaemons(c)}}) {
                if (one[1].isEmpty()) continue;
                HTMLElement line = el("div");
                HTMLElement b = el("b");
                b.textContent = tr(one[0]) + " ";
                line.append(b, DomGlobal.document.createTextNode(one[1]));
                lines.append(line);
            }
        });
        if (!May.can("admin")) return out;
        // 아래 줄은 그 화면이 무엇인지 설명하는 말이다 — 레일의 문에 붙는 한 줄(nav.access_sub)이
        // 아니라, 운영이 이 자리에 쓰는 그 문장(access.why).
        HTMLElement r = row("accessK", "accessWhy", "nav.access", "access.why");
        r.className = "prefrow narrowonly";
        HTMLElement go = el("md-text-button");
        go.id = "accessGo";
        // 버튼의 말은 "연다"이다 — 줄의 제목이 무엇을 여는지 이미 말했고, 제목을 버튼에 한 번
        // 더 적으면 390px에서 글자 기둥이 반으로 눌려 두 줄이 된다(실측: 줄 높이 40 대 67).
        go.textContent = tr("access.open");
        go.setAttribute("aria-label", tr("nav.access"));
        go.addEventListener("click", evt -> dev.sayaya.magi.bridge.GoSharing.view("access"));
        r.append(go);
        add(out, r);
        return out;
    }

    private static String joinDaemons(JsPropertyMap<Object> c) {
        JsArrayLike<Object> all = Js.uncheckedCast(c.get("daemons"));
        if (all == null) return "";
        StringBuilder out = new StringBuilder();
        for (int i = 0; i < all.getLength(); i++) {
            if (out.length() > 0) out.append(", ");
            out.append(String.valueOf(all.getAt(i)));
        }
        return out.toString();
    }

    /** 여러 조각을 한 줄씩 — 무리는 폼의 직계다(익명 상자 한 겹이 구조를 다르게 만든다). */
    private static void add(List<HTMLElement> out, HTMLElement... some) {
        for (HTMLElement one : some) if (one != null) out.add(one);
    }

    private HTMLElement field(String id, String label) {
        HTMLElement f = el("md-outlined-text-field");
        f.id = id;
        f.setAttribute("label", label);
        return f;
    }

    /** 여러 줄을 적는 칸 — 저장은 손을 뗄 때(값이 실제로 바뀌었을 때만). */
    private HTMLElement templateRow(String kId, String kKey, String field, String now) {
        // 쌓는 줄은 .prefsay 하나다 — row()로 만들면 그 안에 .prefsay가 한 겹 더 생겨,
        // 같은 이름이 두 번 세어진다(운영과 견주다 드러난 그 한 겹).
        HTMLElement r = el("div");
        r.className = "prefsay";
        HTMLElement k = el("div");
        k.className = "k";
        k.id = kId;
        k.textContent = tr(kKey);
        r.append(k);
        HTMLElement f = el("md-outlined-text-field");
        f.id = field;
        f.setAttribute("label", tr(kKey));
        f.setAttribute("type", "textarea");
        f.setAttribute("rows", "3");
        Js.asPropertyMap(f).set("value", now == null ? "" : now);
        final String[] saved = {now == null ? "" : now};
        f.addEventListener("blur", evt -> {
            String said = value(f);
            if (said.equals(saved[0])) return;   // 바뀐 적 없는 값을 다시 쓰지 않는다
            saved[0] = said;
            store.save(field, said);
        });
        r.append(f);
        return r;
    }

    private HTMLElement profileRow(String kId, String whyId, String kKey, String whyKey,
                                   String field, String now) {
        HTMLElement r = row(kId, whyId, kKey, whyKey);
        HTMLElement sel = el("md-outlined-select");
        sel.className = "profsel";
        sel.setAttribute("data-field", field);
        // 줄의 제목은 어느 설정인지 말한다(코드 완성 모델) — 칸의 라벨은 고르는 것이 무엇인지
        // 말한다(프로필). 둘이 같은 말이면 한 줄에 같은 말이 두 번 선다(운영의 그 구분).
        sel.setAttribute("label", tr("ac.profile_pick"));
        HTMLElement none = el("md-select-option");
        none.setAttribute("value", "");
        HTMLElement noneHead = el("div");
        noneHead.setAttribute("slot", "headline");
        noneHead.textContent = tr("ac.profile_none");
        none.append(noneHead);
        sel.append(none);
        JsArrayLike<Object> profiles = Js.uncheckedCast(complete().get("profiles"));
        for (int i = 0; profiles != null && i < profiles.getLength(); i++) {
            String name = String.valueOf(profiles.getAt(i));
            HTMLElement opt = el("md-select-option");
            opt.setAttribute("value", name);
            HTMLElement head = el("div");
            head.setAttribute("slot", "headline");
            head.textContent = name;
            opt.append(head);
            sel.append(opt);
        }
        Js.asPropertyMap(sel).set("value", now);
        sel.addEventListener("change", evt -> store.save(field, value(sel)));
        put(r, sel);
        return r;
    }

    private HTMLElement daemonSwitch(String kId, String whyId, String kKey, String whyKey,
                                     boolean on, String field) {
        HTMLElement r = row(kId, whyId, kKey, whyKey);
        HTMLElement sw = el("md-switch");
        sw.setAttribute("touch-target", "wrapper");
        sw.setAttribute("data-field", field);
        Js.asPropertyMap(sw).set("selected", on);
        sw.addEventListener("change", evt ->
                store.save(field, Prefs.word(Js.isTruthy(Js.asPropertyMap(sw).get("selected")))));
        put(r, sw);
        return r;
    }

    private HTMLElement switchRow(String kId, String whyId, String kKey, String whyKey,
                                  String prefKey, boolean byDefault, Kept kept) {
        HTMLElement r = row(kId, whyId, kKey, whyKey);
        HTMLElement sw = el("md-switch");
        sw.setAttribute("touch-target", "wrapper");
        sw.setAttribute("data-pref", prefKey);
        Js.asPropertyMap(sw).set("selected", store.switchOn(prefKey, byDefault));
        sw.addEventListener("change", evt ->
                kept.call(Js.isTruthy(Js.asPropertyMap(sw).get("selected"))));
        put(r, sw);
        return r;
    }

    private interface Kept { void call(boolean on); }

    /** 한 줄: 이름과 그 아래 설명, 그리고 줄 끝의 컨트롤(운영 .prefrow의 그 모양). */
    private HTMLElement row(String kId, String whyId, String kKey, String whyKey) {
        HTMLElement r = el("div");
        r.className = "prefrow";
        HTMLElement say = el("div");
        say.className = "prefsay";
        HTMLElement k = el("div");
        k.className = "k";
        k.id = kId;
        k.textContent = tr(kKey);
        say.append(k);
        if (whyId != null && whyKey != null && !whyKey.isEmpty()) {
            HTMLElement why = el("div");
            why.className = "say";
            why.id = whyId;
            why.textContent = tr(whyKey);
            say.append(why);
        }
        r.append(say);
        return r;
    }

    /**
     * 줄 끝의 컨트롤을 그 줄에 세운다 — <b>이름을 붙여서</b>.
     *
     * 스위치의 보이는 이름은 형제 div이지 &lt;label for&gt;가 아니다: 그냥 append하면 스크린
     * 리더가 이름 없는 "switch"라고 읽는다(운영이 ariaLabel(...)로 메운 그 구멍). 줄의 제목이
     * 곧 그 컨트롤의 이름이라, 한 곳에서 옮겨 붙인다 — 줄마다 지킬 규약을 두지 않는다.
     *
     * 필드류는 aria-label이 아니라 label을 받는다: 눈에 보이는 라벨이 그 컨트롤의 몫이고,
     * 머티리얼의 필드는 그것을 제 자리에 그린다.
     */
    private static void put(HTMLElement row, HTMLElement control) {
        elemental2.dom.Element k = row.querySelector(".k");
        String name = k == null || k.textContent == null ? "" : k.textContent.trim();
        String tag = control.tagName.toLowerCase();
        boolean field = tag.startsWith("md-outlined-");
        if (!name.isEmpty() && !control.hasAttribute(field ? "label" : "aria-label")) {
            control.setAttribute(field ? "label" : "aria-label", name);
        }
        row.append(control);
    }

    private static java.util.List<String> names(JsArrayLike<Object> all) {
        java.util.List<String> out = new java.util.ArrayList<>();
        for (int i = 0; i < all.getLength(); i++) {
            out.add(str(Js.<JsPropertyMap<Object>>uncheckedCast(all.getAt(i)), "name"));
        }
        return out;
    }

    private static java.util.List<String> models(JsPropertyMap<Object> provider) {
        java.util.List<String> out = new java.util.ArrayList<>();
        JsArrayLike<Object> all = provider == null ? null : Js.uncheckedCast(provider.get("models"));
        for (int i = 0; all != null && i < all.getLength(); i++) out.add(String.valueOf(all.getAt(i)));
        return out;
    }

    private static JsPropertyMap<Object> byName(JsArrayLike<Object> all, String name) {
        for (int i = 0; i < all.getLength(); i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            if (str(one, "name").equals(name)) return one;
        }
        return null;
    }

    /** 고를 것들을 다시 적는다 — 첫 자리는 "아직 아무것도"(고르개는 빈 값을 가질 수 있다). */
    private static void fill(HTMLElement sel, java.util.List<String> items, String placeholder) {
        sel.replaceChildren();
        java.util.List<String> all = new java.util.ArrayList<>();
        all.add("");
        all.addAll(items);
        for (String it : all) {
            HTMLElement o = el("md-select-option");
            o.setAttribute("value", it);
            HTMLElement head = el("div");
            head.setAttribute("slot", "headline");
            head.textContent = it.isEmpty() ? placeholder : it;
            o.append(head);
            sel.append(o);
        }
    }

    private HTMLElement group(String id, String words) {
        HTMLElement g = el("div");
        g.className = "prefgroup";
        g.id = id;
        g.textContent = words;
        return g;
    }

    /** 문서에 테마를 적는다 — "기계를 따름"은 적지 않는 것이다(그래야 매체 질의가 답한다). */
    private static void applyTheme(String pref) {
        String attr = Prefs.themeAttribute(pref);
        if (attr == null) DomGlobal.document.documentElement.removeAttribute("color-theme");
        else DomGlobal.document.documentElement.setAttribute("color-theme", attr);
    }

    private JsPropertyMap<Object> complete() {
        return store.complete() == null ? null : Js.uncheckedCast(store.complete());
    }

    private static String nameOf(String socket) {
        int slash = socket.lastIndexOf('/');
        return slash >= 0 ? socket.substring(slash + 1) : socket;
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m == null ? null : m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
