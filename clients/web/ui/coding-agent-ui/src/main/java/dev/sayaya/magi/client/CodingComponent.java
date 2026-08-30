package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.ConversationElement;
import dev.sayaya.magi.client.interfaces.WorkspaceElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = CodingModule.class)
public interface CodingComponent {
    ConversationElement conversation();
    WorkspaceElement workspace();
}
