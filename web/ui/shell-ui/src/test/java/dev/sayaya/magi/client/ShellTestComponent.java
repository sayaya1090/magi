package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.FrameElement;
import dev.sayaya.magi.client.interfaces.MastheadElement;
import dev.sayaya.magi.client.interfaces.RailElement;
import dev.sayaya.magi.client.usecase.ShellInitializer;
import dev.sayaya.magi.client.usecase.ToolList;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = ShellTestModule.class)
public interface ShellTestComponent {
    ShellInitializer initializer();
    ToolList toolList();
    MastheadElement masthead();

    dev.sayaya.magi.client.interfaces.PaletteElement palette();
    RailElement rail();
    FrameElement frame();
}
