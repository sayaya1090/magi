package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * 코어를 받아오는 일이 창 수만큼 일어나던 것을 막는 자리. 실사용 보고: 인텔리제이를 여러 개
 * 켜 놓고 플러그인을 깔았더니 받는다는 대화가 여러 번 떴다.
 */
class OnceAcrossTest {

    /** 창 여섯이 나란히 도착해도 시작하는 것은 하나다. 그리고 **모두가 그 하나의 답을 받는다.** */
    @Test
    fun `여럿이 동시에 와도 한 번만 시작한다`() {
        val once = OnceAcross<String>()
        val starts = AtomicInteger()
        val hold = CompletableFuture<String>()
        val ready = CountDownLatch(6)
        val go = CountDownLatch(1)
        val got = java.util.Collections.synchronizedList(mutableListOf<String>())

        val threads = (1..6).map {
            Thread {
                ready.countDown()
                go.await()
                once.join { f -> starts.incrementAndGet(); hold.whenComplete { v, _ -> f.complete(v) } }
                    .thenAccept { got.add(it) }
            }.also { it.start() }
        }
        ready.await(5, TimeUnit.SECONDS)
        go.countDown()
        threads.forEach { it.join(5_000) }

        assertEquals(1, starts.get(), "여섯이 왔는데 시작이 여러 번이면 대화도 여러 번이다")
        hold.complete("magi")
        Thread.sleep(100)
        assertEquals(6, got.size, "기다린 쪽이 답을 못 받으면 그 창은 데몬을 못 띄운다")
        assertTrue(got.all { it == "magi" }, "받은 답이 저마다면 하나로 모은 뜻이 없다: $got")
    }

    /** 도는 동안 온 사람은 **같은** 것을 받는다 — 새로 만들어 주면 두 벌이 된다. */
    @Test
    fun `도는 동안 부르면 그 비행에 합류한다`() {
        val once = OnceAcross<String>()
        val first = once.join { }
        val second = once.join { throw AssertionError("도는 중인데 또 시작했다") }
        assertSame(first, second)
        assertTrue(once.inFlight())
    }

    /** 끝나면 자리가 빈다. 안 그러면 한 번 실패한 기계는 영영 다시 못 받는다. */
    @Test
    fun `끝나면 다음 사람이 새로 시작할 수 있다`() {
        val once = OnceAcross<String>()
        val starts = AtomicInteger()
        once.join { f -> starts.incrementAndGet(); f.complete("a") }
        assertTrue(!once.inFlight(), "완료했는데 자리가 안 비었다")
        val again = once.join { f -> starts.incrementAndGet(); f.complete("b") }
        assertEquals(2, starts.get(), "앞의 비행이 끝났으면 새 비행이 시작돼야 한다")
        assertEquals("b", again.get(1, TimeUnit.SECONDS))
    }

    /** 실패도 끝이다. 실패한 비행이 자리를 붙들고 있으면 재시도가 막힌다. */
    @Test
    fun `실패해도 자리는 비워진다`() {
        val once = OnceAcross<String>()
        val failed = once.join { f -> f.completeExceptionally(IllegalStateException("네트워크")) }
        assertTrue(failed.isCompletedExceptionally)
        assertTrue(!once.inFlight())
        assertEquals("b", once.join { f -> f.complete("b") }.get(1, TimeUnit.SECONDS))
    }

    /** [OnceAcross.join] 에 넘긴 블록이 그 자리에서 터져도 부르는 쪽은 답을 받는다(예외로). */
    @Test
    fun `시작하다 터지면 그 예외가 결과로 온다`() {
        val once = OnceAcross<String>()
        val f = once.join { throw IllegalStateException("자산 이름을 못 만들었다") }
        assertTrue(f.isCompletedExceptionally)
        assertTrue(!once.inFlight(), "터진 비행이 자리를 붙들면 그 IDE 는 영영 못 받는다")
    }

    /** null 도 답이다 — 「사람이 미뤘다」를 실패와 가르는 자리라 값으로 실려야 한다. */
    @Test
    fun `null 도 결과로 나른다`() {
        val once = OnceAcross<String?>()
        val f = once.join { it.complete(null) }
        assertNull(f.get(1, TimeUnit.SECONDS))
        assertNotNull(once)
    }
}
