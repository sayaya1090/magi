package dev.sayaya.magi.client.interfaces;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 이 브라우저가 알림을 받을 수 있는가, 그리고 받겠다고 등록하는 일 — 전부 브라우저의 것이라
 * 여기(interfaces)에 둔다.
 *
 * 왜 화면이 이 일을 직접 하나: 구독은 <b>이 브라우저</b>의 사실이다. 서버는 그 결과(끝점과
 * 열쇠)를 받아 둘 뿐이고, 어떤 창이 구독했는지는 창만 안다. 그래서 서비스 워커 등록도
 * 권한 묻기도 이 자리에 있고, 스토어는 그 답을 서버에 올리는 일만 한다.
 */
@Singleton
public class Notifications {
    @Inject
    public Notifications() {}

    /** 왜 지금 켤 수 없는가 — 켤 수 있으면 빈 문자열(팩 키를 돌려준다). */
    public String blocked() {
        if (!secure()) return "notify.insecure";
        if (!supported()) return "notify.unsupported";
        if ("denied".equals(permission())) return "notify.denied";
        return "";
    }

    @JsFunction
    public interface Landed { void call(String whyOrEmpty, String endpoint, String p256dh, String auth); }

    /** 켠다: 권한을 묻고, 워커를 세우고, 구독한다. 답은 서버에 올릴 세 조각이다. */
    public native void turnOn(String vapidKey, Landed landed) /*-{
        // ⚠ JSNI의 파서는 옛 문법만 안다 — async/화살표는 여기서 문법 오류다(실측).
        var keyBytes = function (k) {
            var pad = '==='.substring((k.length + 3) % 4);
            var b = $wnd.atob(k.replace(/-/g, '+').replace(/_/g, '/') + pad);
            var out = new Uint8Array(b.length);
            for (var i = 0; i < b.length; i++) out[i] = b.charCodeAt(i);
            return out;
        };
        var fail = function (e) { landed(String((e && e.message) || e), '', '', ''); };
        try {
            $wnd.Notification.requestPermission().then(function (asked) {
                if (asked !== 'granted') { landed('notify.denied', '', '', ''); return; }
                $wnd.navigator.serviceWorker.register('/sw.js').then(function (reg) {
                    return $wnd.navigator.serviceWorker.ready.then(function () {
                        return reg.pushManager.subscribe({
                            userVisibleOnly: true, applicationServerKey: keyBytes(vapidKey)});
                    });
                }).then(function (sub) {
                    var j = sub.toJSON();
                    landed('', j.endpoint, j.keys.p256dh, j.keys.auth);
                })["catch"](fail);
            })["catch"](fail);
        } catch (e) { fail(e); }
    }-*/;

    /** 끈다: 이 브라우저의 구독을 걷는다. 끝점은 서버에서도 지우라고 올릴 값이다. */
    public native void turnOff(Landed landed) /*-{
        var fail = function (e) { landed(String((e && e.message) || e), '', '', ''); };
        try {
            $wnd.navigator.serviceWorker.getRegistration().then(function (reg) {
                if (!reg) { landed('', '', '', ''); return null; }
                return reg.pushManager.getSubscription().then(function (sub) {
                    if (!sub) { landed('', '', '', ''); return null; }
                    var endpoint = sub.endpoint;
                    return sub.unsubscribe().then(function () {
                        landed('', endpoint, '-', '-');
                    });
                });
            })["catch"](fail);
        } catch (e) { fail(e); }
    }-*/;

    /** 지금 이 브라우저가 구독 중인가 — 화면이 스위치를 그 사실에 맞춘다. */
    public native void subscribed(SubscribedFn then) /*-{
        try {
            if (!('serviceWorker' in $wnd.navigator) || !('PushManager' in $wnd)) { then(false); return; }
            // ⚠ .catch 도 옛 파서에게는 예약어다 — 대괄호로 부른다(위 실측).
            $wnd.navigator.serviceWorker.getRegistration().then(function (reg) {
                if (!reg) { then(false); return null; }
                return reg.pushManager.getSubscription().then(function (sub) { then(!!sub); });
            })["catch"](function () { then(false); });
        } catch (e) { then(false); }
    }-*/;

    @JsFunction
    public interface SubscribedFn { void call(boolean on); }

    private static native boolean secure() /*-{
        return !!$wnd.isSecureContext;
    }-*/;

    private static native boolean supported() /*-{
        return ('serviceWorker' in $wnd.navigator) && ('PushManager' in $wnd);
    }-*/;

    private static native String permission() /*-{
        return ($wnd.Notification && $wnd.Notification.permission) || 'default';
    }-*/;
}
