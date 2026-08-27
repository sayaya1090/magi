package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.FrameElement;
import dev.sayaya.magi.client.interfaces.MastheadElement;
import dev.sayaya.magi.client.interfaces.RailElement;
import dev.sayaya.magi.client.usecase.ShellInitializer;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = ShellModule.class)
public interface ShellComponent {
    ShellInitializer initializer();
    MastheadElement masthead();
    dev.sayaya.magi.client.interfaces.TurnbarElement turnbar();

    dev.sayaya.magi.client.interfaces.PaletteElement palette();
    RailElement rail();
    FrameElement frame();
}
