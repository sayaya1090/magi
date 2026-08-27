package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Tree;
import org.junit.jupiter.api.Test;

import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

import static org.junit.jupiter.api.Assertions.assertEquals;

class TreeTest {
    @Test
    void rootIsTheBareName() {
        assertEquals("src", Tree.childPath(".", "src"));
        assertEquals("src/main", Tree.childPath("src", "main"));
    }

    @Test
    void onlyOpenDirectoriesUnderOpenParentsAreRead() {
        Set<String> open = new LinkedHashSet<>(List.of("src", "src/main", "docs/deep"));
        List<String> want = Tree.wanted(open);
        // docs 가 닫혀 있으니 docs/deep 은 아무도 볼 수 없는 걸음이다.
        assertEquals(List.of(".", "src", "src/main"), want);
    }

    @Test
    void stagedIsStagedOrBoth() {
        assertEquals(true, Tree.staged("staged"));
        assertEquals(true, Tree.staged("both"));
        assertEquals(false, Tree.staged("worktree"));
    }

    @Test
    void 판의_머리는_마지막_두_마디다() {
        // 마지막 한 마디만 남기면 작업공간이 여럿일 때 같은 제목이 여럿 생긴다.
        assertEquals("tmp/ws1", Tree.shortPath("/tmp/ws1"));
        assertEquals("work/thing", Tree.shortPath("/Users/somebody/work/thing"));
        assertEquals("ws1", Tree.shortPath("ws1"));
        assertEquals("", Tree.shortPath(""));
        assertEquals("tmp/ws1", Tree.shortPath("/tmp/ws1/"));
    }
}