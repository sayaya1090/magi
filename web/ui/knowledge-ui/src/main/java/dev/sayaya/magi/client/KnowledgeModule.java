package dev.sayaya.magi.client;

import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchKnowledgeSource;
import dev.sayaya.magi.client.usecase.KnowledgeSource;


/** 포트에 회선을 문다 — 데모면 이 모듈이 함께 싣는 제 목을 문다(테스트는 가짜를 문다). */
@Module
public abstract class KnowledgeModule {
    /** 데모냐 아니냐는 여기서 묻지 않는다 — 회선의 성질이고, 그 자리에 목이 선다(demo-ui). */
    @dagger.Binds
    abstract KnowledgeSource source(FetchKnowledgeSource impl);
}
