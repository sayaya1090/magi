package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.BoardElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = BoardTestModule.class)
public interface BoardTestComponent {
    BoardElement boardElement();
}
