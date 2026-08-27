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
}
