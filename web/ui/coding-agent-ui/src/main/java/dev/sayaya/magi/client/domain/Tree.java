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
