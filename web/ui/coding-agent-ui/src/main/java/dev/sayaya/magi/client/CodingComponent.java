package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.ConversationElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = CodingModule.class)
public interface CodingComponent {
    ConversationElement conversation();
}
