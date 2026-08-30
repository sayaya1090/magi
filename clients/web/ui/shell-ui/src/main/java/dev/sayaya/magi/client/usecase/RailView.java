package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;

/** 드로어가 유스케이스에 보이는 면: 어디가 선택됐는지 그리는 것뿐. */
public interface RailView {
    void select(Destination d);
}
