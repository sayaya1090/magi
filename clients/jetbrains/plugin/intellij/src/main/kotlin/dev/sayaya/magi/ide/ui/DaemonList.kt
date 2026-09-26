package dev.sayaya.magi.ide.ui

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.stateIn

/**
 * Something the daemon answers — the conversation list, the model list — kept as state rather than
 * fetched once.
 *
 * The callback version this replaces read each list when the window opened and dropped the failure
 * (`onDaemon({}) { … }`). A daemon still starting at that moment left the combo enabled and empty for
 * the life of the window, because the retry tick only revived *disabled* combos — measured on a real
 * screen (2026-09-26): the daemon answered three conversations, the combo showed none. Here a read that
 * fails keeps trying on its own, with backoff, until it lands or a newer request replaces it; nothing
 * has to remember to ask again.
 *
 * [read] throws when the daemon could not be reached — that is what gets retried. An answer that is
 * final ("this daemon has no such door") is a value of [T], not an exception, so it is not retried.
 */
@OptIn(ExperimentalCoroutinesApi::class)
internal class DaemonList<T>(
    scope: CoroutineScope,
    private val retryAfterMs: (attempt: Int) -> Long = { minOf(500L shl minOf(it, 4), 5_000L) },
    private val read: suspend () -> T,
) {
    sealed interface State<out T> {
        data object Loading : State<Nothing>
        data class Ready<T>(val value: T) : State<T>
        /** The latest attempt failed; another is already scheduled. */
        data class Failed(val why: String) : State<Nothing>
    }

    // One pending request is enough: asking twice before the first read starts is asking once.
    private val asks = MutableSharedFlow<Unit>(replay = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)

    val state: StateFlow<State<T>> = asks
        .flatMapLatest {
            flow<State<T>> {
                var attempt = 0
                while (true) {
                    try {
                        emit(State.Ready(read()))
                        return@flow
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        emit(State.Failed(e.message ?: e.javaClass.simpleName))
                        delay(retryAfterMs(attempt++))
                    }
                }
            }
        }
        .stateIn(scope, SharingStarted.Eagerly, State.Loading)

    /** Read again — on opening the list, after a verb that changed it. A newer request replaces an older one. */
    fun refresh() {
        asks.tryEmit(Unit)
    }

    init {
        refresh()
    }
}
