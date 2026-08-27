package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchKnowledgeSource;
import dev.sayaya.magi.client.usecase.KnowledgeSource;

/** 유스케이스 포트 → interfaces 구현 바인딩. 테스트는 같은 자리에 가짜를 물린다. */
@Module
public abstract class KnowledgeModule {
    @Binds abstract KnowledgeSource source(FetchKnowledgeSource impl);
}
