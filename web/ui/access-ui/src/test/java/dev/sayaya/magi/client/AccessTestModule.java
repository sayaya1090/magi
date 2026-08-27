package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.AccessSource;

@Module
public abstract class AccessTestModule {
    @Binds abstract AccessSource source(FakeAccessSource impl);
}
