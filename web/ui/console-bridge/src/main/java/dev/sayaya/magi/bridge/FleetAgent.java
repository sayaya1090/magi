package dev.sayaya.magi.bridge;

import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;

/**
 * BFF /fleet 행 하나 — internal/adapter/fleet.Agent의 JSON 표현 전체.
 * 필드명은 fleet.go의 json 태그와 일대일. omitempty 필드는 JS에서 undefined일 수 있으니
 * 읽는 쪽이 null 가드를 진다(GWT에서 미존재 프로퍼티 읽기는 null/0/false).
 */
@JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
public class FleetAgent {
    public String peer;
    public String socket;
    public String workdir;
    public String name;
    public String session;
    public int pid;
    public String role;
    public String team;
    public boolean hub;
    public String host;
    public String version;
    public boolean elsewhere;
    public String trust;
    public String instance;
    public String addr;
    public boolean live;
    public String state;
    public String asking;
    public String askId;
    public String askKind;
    public int askIndex;
    public int askTotal;
    public String[] askOptions;
    public ReportSection[] report;
    public String task;
    public int waiting;      // 이웃이 넘긴 것 중 줄 서 있는 수
    public boolean handling; // 하나는 손에 들고 있음
    public int steps;
    public String doing;
    public String permission;
    public String backend;
    public String user;
    public int planDone;
    public int planTotal;
    public String model;
    public int idle;         // 마지막 이벤트 이후 초; 모르면 -1
    public boolean here;

    /** 결정 보고서의 한 섹션 (internal/core/report.Filled). */
    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
    public static class ReportSection {
        public String key;
        public String text;
    }
}
