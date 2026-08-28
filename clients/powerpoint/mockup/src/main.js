// 조립하는 자리. **여기만 무엇이 무엇인지 안다** — 안쪽 층은 서로를 인터페이스로만 안다.
import { Conversation } from './domain/Conversation.js';
import { QuoteSelection } from './usecase/QuoteSelection.js';
import { PointAtAdvice } from './usecase/PointAtAdvice.js';
import { SendTurn } from './usecase/SendTurn.js';
import { OfficeDeck } from './adapter/OfficeDeck.js';
import { FakeDeck } from './adapter/FakeDeck.js';
import { FakeChat } from './adapter/FakeChat.js';
import { View } from './ui/view.js';
import { mountFakeCanvas } from './ui/fakeCanvas.js';
import { fixture } from './ui/deckFixture.js';

/**
 * 어느 덱에 붙을지 고른다.
 *
 * Office 밖에서는 `Office.onReady()` 가 **영영 안 풀린다.** 그래서 기다리기만 하면 빈 화면으로
 * 멈춘다 — 경주를 붙여 1.5초 뒤에는 가짜로 간다. 넘겨짚는 게 아니라 **못 정했다는 것을 정하는**
 * 것이고, 화면이 어느 쪽인지 그대로 띄운다.
 */
async function pickDeck() {
  if (typeof Office === 'undefined') return new FakeDeck(fixture);
  try {
    const host = await Promise.race([
      Office.onReady().then((info) => info?.host ?? null),
      new Promise((r) => setTimeout(() => r(null), 1500)),
    ]);
    if (host === Office.HostType.PowerPoint) return new OfficeDeck();
  } catch { /* Office 밖에서 office.js 가 던지는 경우 */ }
  return new FakeDeck(fixture);
}

async function boot() {
  const deck = await pickDeck();
  const conversation = new Conversation();
  const chat = new FakeChat();

  const view = new View({
    conversation,
    quoteSelection: new QuoteSelection(deck, conversation),
    pointAt: new PointAtAdvice(deck),
    sendTurn: new SendTurn(chat, conversation),
    chat,
    deck,
  });
  view.mount();

  if (deck instanceof FakeDeck) {
    document.body.classList.add('standalone');
    mountFakeCanvas(deck, document.querySelector('#fake'));
  }
}

boot();
