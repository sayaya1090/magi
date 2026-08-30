package dev.sayaya.magi.client.domain;

import java.util.ArrayList;
import java.util.List;
import java.util.Set;

/**
 * 트리의 순수 규칙 — 운영 walkTree/branches/wantedDirs의 뼈: 어떤 디렉토리를 읽어야 하고,
 * 무엇을 어느 깊이로 그릴지. DOM도 회선도 모른다.
 */
public final class Tree {
    private Tree() {}

    /**
     * 판의 머리에 적히는 이름 — 마지막 <b>두</b> 마디(운영 shortPath).
     *
     * 한 마디면 `ws1`인데, 여러 작업공간의 마지막 마디가 겹치는 일이 흔하다(build/·src/·app/):
     * 그 제목은 어느 것인지 말하지 못한다. 두 마디는 `tmp/ws1`이고 좁은 기둥에도 들어간다.
     * 절대경로 전체는 판 폭을 다 먹고도 "thing" 한 마디만 말한다 — 그래서 접는다.
     */
    public static String shortPath(String path) {
        if (path == null || path.isEmpty()) return "";
        String[] parts = path.split("/");
        StringBuilder tail = new StringBuilder();
        int taken = 0;
        for (int i = parts.length - 1; i >= 0 && taken < 2; i--) {
            if (parts[i].isEmpty()) continue;
            tail.insert(0, taken == 0 ? parts[i] : parts[i] + "/");
            taken++;
        }
        return tail.length() == 0 ? path : tail.toString();
    }

    /** 자식의 경로 — 뿌리 밑은 이름 그대로다(운영과 같은 규약: 뿌리는 "."). */
    public static String childPath(String dir, String name) {
        return ".".equals(dir) || dir == null || dir.isEmpty() ? name : dir + "/" + name;
    }

    /**
     * 다음 걸음이 읽어야 할 디렉토리들 — 뿌리와, **열린 부모 아래에 있는** 열린 디렉토리만.
     * 부모가 닫혀 화면에 없는 자식을 읽는 것은 아무도 볼 수 없는 걸음이다(운영 wantedDirs).
     */
    public static List<String> wanted(Set<String> open) {
        List<String> want = new ArrayList<>();
        want.add(".");
        for (String p : open) {
            String[] parts = p.split("/");
            boolean ok = true;
            for (int i = 1; i < parts.length; i++) {
                StringBuilder parent = new StringBuilder(parts[0]);
                for (int j = 1; j < i; j++) parent.append('/').append(parts[j]);
                if (!open.contains(parent.toString())) { ok = false; break; }
            }
            if (ok) want.add(p);
        }
        return want;
    }

    /** 변경 하나가 커밋에 실리는가 — staged와 both가 그 반쪽이다(운영 규칙). */
    public static boolean staged(String kind) {
        return "staged".equals(kind) || "both".equals(kind);
    }
}
