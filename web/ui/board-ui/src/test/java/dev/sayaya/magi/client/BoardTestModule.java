package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.BoardSource;

@Module
public abstract class BoardTestModule {
    @Binds abstract BoardSource source(FakeBoardSource impl);
}
