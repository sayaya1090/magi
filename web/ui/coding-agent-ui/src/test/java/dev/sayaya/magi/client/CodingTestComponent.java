package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.ConversationElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = CodingTestModule.class)
public interface CodingTestComponent {
    ConversationElement conversation();
}
