package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.MapSource;

@Module
public abstract class MapTestModule {
    @Binds abstract MapSource source(FakeMapSource impl);
}
