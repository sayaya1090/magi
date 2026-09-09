package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.Told;
import dev.sayaya.magi.client.domain.Tongue;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * 지금 그리는 말의 팩.
 *
 * 말을 갈면 팩을 한 번 더 읽고, 한 번 읽은 팩은 들고 있는다 — 두 말을 오가는 사람이 오갈 때마다
 * 회선을 태우지 않게. 팩이 도착하면 소식만 흘리고(Told), 다시 그리는 것은 판의 몫이다.
 */
@Singleton
public class WordStore extends Told {
    private final WordSource source;
    private final TongueStore tongues;
    private final Map<String, Map<String, String>> read = new LinkedHashMap<>();
    private boolean started = false;

    @Inject
    public WordStore(WordSource source, TongueStore tongues) {
        this.source = source;
        this.tongues = tongues;
    }

    public void start() {
        if (started) return;
        started = true;
        // 사람이 말을 갈면 그 말의 팩이 필요하다 — 없으면 읽고, 있으면 그 자리에서 다시 그린다.
        tongues.onChange(this::fetch);
        fetch();
    }

    private void fetch() {
        String code = tongues.now().code();
        if (read.containsKey(code)) { told(); return; }
        source.load(code, pack -> {
            // 못 읽었어도 자리를 채워 둔다: 같은 실패를 말을 오갈 때마다 다시 시도하지 않는다.
            read.put(code, pack == null ? new LinkedHashMap<>() : pack);
            told();
        });
    }

    /** 팩이 아직인가 — 판은 그동안 아무것도 그리지 않는다(키 문자열이 깜빡이지 않게). */
    public boolean ready() { return read.containsKey(tongues.now().code()); }

    public Tongue now() { return tongues.now(); }

    /**
     * 그 낱말. 모르는 키는 <b>키 그대로</b> 돌려준다 — 콘솔의 tr() 과 같은 규칙이고, 빠진 말이
     * 빈칸이 아니라 눈에 띄는 문자열로 서게 하려는 것이다.
     */
    public String word(String key) {
        Map<String, String> pack = read.get(tongues.now().code());
        if (pack == null) return key;
        String said = pack.get(key);
        return said == null || said.isEmpty() ? key : said;
    }
}
