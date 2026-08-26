package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.FrameElement;
import dev.sayaya.magi.client.interfaces.RailElement;
import dev.sayaya.magi.client.usecase.FrameView;
import dev.sayaya.magi.client.usecase.ModuleLoader;
import dev.sayaya.magi.client.usecase.RailView;
import dev.sayaya.magi.client.usecase.RosterSource;

@Module
public abstract class ShellTestModule {
    @Binds abstract ModuleLoader loader(FakeModuleLoader impl);
    @Binds abstract RailView rail(RailElement impl);
    @Binds abstract FrameView frame(FrameElement impl);
    @Binds abstract RosterSource roster(FakeRosterSource impl);
}
