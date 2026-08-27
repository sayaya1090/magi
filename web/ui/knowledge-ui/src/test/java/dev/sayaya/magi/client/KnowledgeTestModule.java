package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.KnowledgeSource;

@Module
public abstract class KnowledgeTestModule {
    @Binds abstract KnowledgeSource source(FakeKnowledgeSource impl);
}
