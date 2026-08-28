package dev.sayaya.magi.demo;

import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 이 콘솔과 데몬의 설정 — 완성 설정, 모델 프로파일, 서빙 중인 백엔드.
 *
 * 프로파일 셋 중 하나에는 키가 있다: "정해졌지만 보여 주지는 않는" 상태가 이 화면의 계약이라
 * (값은 어느 화면에도 오지 않고 있다는 사실만 온다) 그것을 보이는 픽스처가 필요하다.
 */
final class Settings {
    private Settings() {}

    static Promise<Response> answer(String path, RequestInit init) {
        switch (path) {
            case "/autocomplete":
                if (Mock.wrote(init)) return Mock.json("");
                return Mock.json("{\"file\":\"~/.config/magi/config.toml\","
                        + "\"ambient\":true,\"crossSession\":false,"
                        + "\"profiles\":[\"fast-local\",\"cloud-mini\"],"
                        + "\"codeProfile\":\"fast-local\",\"composerProfile\":\"\"}");
            case "/profiles":
                if (Mock.wrote(init)) return Mock.json("");
                return Mock.json(
                        "[{\"name\":\"balanced\",\"tier\":\"global\",\"baseUrl\":\"http://localhost:11434/v1\","
                        + "\"model\":\"qwen3-coder:30b\",\"hasKey\":false,\"file\":\"~/.config/magi/config.toml\"},"
                        + "{\"name\":\"fast\",\"tier\":\"global\",\"baseUrl\":\"http://localhost:11434/v1\","
                        + "\"model\":\"qwen2.5-coder:1.5b\",\"hasKey\":false,\"file\":\"~/.config/magi/config.toml\"},"
                        + "{\"name\":\"cloud\",\"tier\":\"project\",\"companion\":\"design\","
                        + "\"socket\":\"/demo/design.sock\",\"baseUrl\":\"https://api.example.com/v1\","
                        + "\"model\":\"big-model\",\"hasKey\":true,"
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"}]");
            // 짧은 카탈로그 하나 — 두 고르개(백엔드·모델)가 하는 일이 보일 만큼.
            case "/providers": return Mock.json(
                    "[{\"name\":\"gateway\",\"base\":\"http://127.0.0.1:47311/v1\","
                    + "\"models\":[\"fast\",\"balanced\",\"deep\"]}]");
            // 데모에는 뒤에 함대를 보는 것이 없다 — 키가 없다고 답하고, 화면이 그 사실을 적는다.
            case "/push": return Mock.json(Mock.wrote(init) ? "" : "{\"key\":\"\"}");
            default: return null;
        }
    }
}
