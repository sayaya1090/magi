package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.BoardElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = BoardModule.class)
public interface BoardComponent {
    BoardElement boardElement();
}
