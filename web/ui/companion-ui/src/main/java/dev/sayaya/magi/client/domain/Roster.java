package dev.sayaya.magi.client.domain;

import dev.sayaya.magi.bridge.AgentStates;

import dev.sayaya.magi.bridge.FleetAgent;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 명단을 읽는 법 — 세기, 거르기, 줄 세우기, 팀으로 묶기. DOM을 모른다.
 *
 * 정렬 규칙: 골칫거리 먼저, 그룹 안에서는 최근 활동순, 딴 데(elsewhere) 것은 맨 뒤 —
 * 남의 콘솔이 답할 문제를 이 화면 꼭대기에 올리지 않는다. 팀 그룹은 제일 시끄러운
 * 구성원이 자리를 정하고(막힌 팀이 먼저), 무명 그룹은 늘 마지막이다.
 */
public final class Roster {
    private Roster() {}

    /** 사람을 기다리는 행 수 — 문서 제목과 배지가 실어 나르는 그 수. */
    public static int waiting(FleetAgent[] list) {
        int n = 0;
        for (FleetAgent a : list) if ("waiting".equals(a.state)) n++;
        return n;
    }

    /** 플릿에서 가장 새 빌드 — 뒤처진 행의 비교 기준. */
    public static String newest(FleetAgent[] list) {
        String top = "";
        for (FleetAgent a : list) if (a.version != null && Versions.compare(a.version, top) > 0) top = a.version;
        return top;
    }

    /** 요약 타일의 네 수. 이 콘솔이 직접 잰 것만 — elsewhere는 남의 콘솔의 측정값이다. */
    public static Map<String, Integer> counts(FleetAgent[] list) {
        Map<String, Integer> counts = new LinkedHashMap<>();
        counts.put("waiting", 0); counts.put("working", 0); counts.put("idle", 0); counts.put("gone", 0);
        for (FleetAgent a : list) {
            if (a.elsewhere) continue;
            String g = AgentStates.groupOf(a.state);
            counts.merge(counts.containsKey(g) ? g : "idle", 1, Integer::sum);
        }
        return counts;
    }

    /** 거르고 줄 세운 행들. filter는 요약 그룹 키 하나거나 null(전부). */
    public static List<FleetAgent> rows(FleetAgent[] list, String filter) {
        List<FleetAgent> rows = new ArrayList<>();
        for (FleetAgent a : list) {
            if (filter == null || (!a.elsewhere && filter.equals(AgentStates.groupOf(a.state)))) rows.add(a);
        }
        rows.sort((x, y) -> {
            int d = Boolean.compare(x.elsewhere, y.elsewhere);
            if (d != 0) return d;
            d = AgentStates.orderOf(x.state) - AgentStates.orderOf(y.state);
            return d != 0 ? d : Integer.compare(x.idle, y.idle);
        });
        return rows;
    }

    /** 팀 헤더를 그릴 이유가 있는가 — 아무도 팀을 선언 안 한 명단엔 가구를 놓지 않는다. */
    public static boolean teamed(List<FleetAgent> rows) {
        for (FleetAgent a : rows) if (a.team != null && !a.team.isEmpty()) return true;
        return false;
    }

    /** 행 순서를 지킨 채 팀으로 묶고, 그룹을 시끄러운 순서로 놓는다. 무명은 마지막. */
    public static List<Team> teams(List<FleetAgent> rows) {
        Map<String, List<FleetAgent>> byName = new LinkedHashMap<>();
        for (FleetAgent a : rows) byName.computeIfAbsent(a.team == null ? "" : a.team, k -> new ArrayList<>()).add(a);
        List<Team> out = new ArrayList<>();
        byName.forEach((name, members) -> out.add(new Team(name, members)));
        out.sort((x, y) -> {
            boolean xu = x.name.isEmpty(), yu = y.name.isEmpty();
            if (xu != yu) return xu ? 1 : -1;
            int d = loudest(x.members) - loudest(y.members);
            return d != 0 ? d : x.name.compareTo(y.name);
        });
        return out;
    }

    private static int loudest(List<FleetAgent> g) {
        int min = Integer.MAX_VALUE;
        for (FleetAgent a : g) min = Math.min(min, AgentStates.orderOf(a.state));
        return min;
    }

    /** 한 팀: 이름과 구성원, 그리고 헤더가 말할 두 사실(대변자들, 기다리는 수). */
    public static final class Team {
        public final String name;
        public final List<FleetAgent> members;

        Team(String name, List<FleetAgent> members) { this.name = name; this.members = members; }

        /** 팀을 대변한다고 주장하는 전원 — 둘이면 설정 오류고, 하나만 그리면 정돈된 척이 된다. */
        public List<String> hubs() {
            List<String> hubs = new ArrayList<>();
            for (FleetAgent a : members) if (a.hub) hubs.add(a.name);
            return hubs;
        }

        public int waiting() {
            int n = 0;
            for (FleetAgent a : members) if ("waiting".equals(a.state)) n++;
            return n;
        }
    }
}
