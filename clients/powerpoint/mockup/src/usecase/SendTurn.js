/**
 * 사용자 차례를 낸다.
 *
 * **던지고 즉시 돌아온다.** 여기서 답을 기다리면 같은 헬퍼를 지나는 모델의 도구 호출과 서로
 * 기다리게 된다(§5.7). 답은 구독으로 온다.
 */
export class SendTurn {
  constructor(chat, conversation) {
    this.chat = chat;
    this.conversation = conversation;
  }

  async run(text) {
    if (!this.conversation.canSend(text)) return null;
    const turn = this.conversation.say(text.trim());
    await this.chat.submit(turn.text, turn.quotes);
    return turn;
  }
}
